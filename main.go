package main

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/url"
	"os"
	"os/signal"
	"path"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/fatih/structs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/joho/godotenv"
	"github.com/mitchellh/mapstructure"
	"github.com/the-rileyj/uyghurs"
	"gopkg.in/yaml.v2"
)

func parseDockerComposeYML(dockerComposeBytes []byte) (uyghurs.ProjectMetadata, error) {
	var hongKongSettings uyghurs.HongKongSettings

	err := yaml.Unmarshal(dockerComposeBytes, &hongKongSettings)

	if err != nil {
		return uyghurs.ProjectMetadata{}, err
	}

	return hongKongSettings.HongKongProjectSettings, nil
}

// func tester(GitlabCIBytes []byte) (*GitlabCI, error) {
// 	GitlabCIMap := make(map[string]interface{})

// 	err := yaml.Unmarshal(GitlabCIBytes, &GitlabCIMap)

// 	if err != nil {
// 		return nil, err
// 	}

// 	gitlabCI := GitlabCI{
// 		BuildItems: make(map[string]*BuildItem),
// 		Variables:  make(map[string]string),
// 	}

// 	for key, value := range GitlabCIMap {
// 		if key == ".tags" {
// 			continue
// 		}

// 		if key == "image" {
// 			realValue, ok := value.(string)

// 			if !ok {
// 				return nil, errors.New(`could not parse "image" value from interface{}`)
// 			}

// 			gitlabCI.Image = realValue
// 		} else if key == "stages" {
// 			realValues, ok := value.([]interface{})

// 			if !ok {
// 				return nil, errors.New(`could not parse "stages" value from interface{}`)
// 			}

// 			stages, err := parseStringSlice(realValues)

// 			if err != nil {
// 				return nil, err
// 			}

// 			gitlabCI.Stages = stages
// 		} else if key == "variables" {
// 			variableMap, ok := value.(map[interface{}]interface{})

// 			if !ok {
// 				return nil, errors.New(`could not parse build item "variables" value from interface{}`)
// 			}

// 			for vKey, vValue := range variableMap {
// 				actualKey, ok := vKey.(string)

// 				if !ok {
// 					return nil, errors.New(`could not parse build item "variable" key value from interface{}`)
// 				}

// 				actualValue, ok := vValue.(string)

// 				if !ok {
// 					return nil, errors.New(`could not parse build item "variable" value from interface{}`)
// 				}

// 				gitlabCI.Variables[actualKey] = actualValue
// 			}
// 		} else {

// 			buildItemInterfaceMap, ok := value.(map[interface{}]interface{})

// 			if !ok {
// 				return nil, errors.New(`could not parse "build item" value from interface{}`)
// 			}

// 			buildItemMap := make(map[string]interface{})

// 			for biKey, biValue := range buildItemInterfaceMap {
// 				biKeyString, ok := biKey.(string)

// 				if ok {
// 					buildItemMap[biKeyString] = biValue
// 				}
// 			}

// 			buildItem, err := parseBuildItem(buildItemMap)

// 			if err != nil {
// 				return nil, err
// 			}

// 			buildItem.Name = key

// 			gitlabCI.BuildItems[key] = buildItem
// 		}
// 	}

// 	return &gitlabCI, nil
// }

func ObjectFileToTarHeader(o *object.File) *tar.Header {
	return &tar.Header{
		Name:     o.Name,
		Mode:     0600,
		Size:     o.Size,
		Typeflag: tar.TypeReg,
	}
}

func EntryDirToTarHeader(n string) *tar.Header {
	return &tar.Header{
		Name:     n + "/",
		Mode:     0600,
		Typeflag: tar.TypeDir,
	}
}

func buildImageForApp(cli *client.Client, githubRepo uyghurs.GithubPush) (*uyghurs.ProjectMetadata, error) {
	memoryStorage := memory.NewStorage()

	// authMethod, err := ssh.DefaultAuthBuilder(ssh.DefaultUsername)

	// if err != nil {
	// 	return err
	// }

	r, err := git.Clone(
		memoryStorage,
		nil,
		&git.CloneOptions{
			URL: githubRepo.Repository.URL,
			// URL: githubRepo.Repository.SSHURL,
			// Auth: authMethod,
		},
	)

	fmt.Println("Cloned into memory")

	headCommitRef, err := r.Head()

	if err != nil {
		return nil, err
	}

	headCommit, err := r.CommitObject(headCommitRef.Hash())

	if err != nil {
		return nil, err
	}

	headTree, err := headCommit.Tree()

	if err != nil {
		return nil, err
	}

	// tar up repo for docker image building
	var tarBuffer bytes.Buffer
	tarWriter := tar.NewWriter(&tarBuffer)

	entryPaths := make([]string, 0)

	var hongKongSettings uyghurs.ProjectMetadata

	handleEntry := func(entry object.TreeEntry, entryName string) error {
		if !entry.Mode.IsFile() {
			entryFileHeader := EntryDirToTarHeader(entry.Name)

			if err := tarWriter.WriteHeader(entryFileHeader); err != nil {
				return err
			}

			entryPaths = append(entryPaths, entryName)
		} else {
			entryFile, err := headTree.File(entryName)

			if err != nil {
				return err
			}

			entryFileHeader := ObjectFileToTarHeader(entryFile)

			if err := tarWriter.WriteHeader(entryFileHeader); err != nil {
				return err
			}

			entryFileReader, err := entryFile.Reader()

			if err != nil {
				return err
			}

			if entryFile.Name == "docker-compose.yml" {
				dockerComposeBytes, err := ioutil.ReadAll(entryFileReader)

				if err != nil {
					panic(err)
				}

				hongKongSettings, err = parseDockerComposeYML(dockerComposeBytes)

				if err != nil {
					return err
				}

				tarWriter.Write(dockerComposeBytes)
			} else {
				io.Copy(tarWriter, entryFileReader)
			}
		}

		return nil
	}

	for _, entry := range headTree.Entries {
		err = handleEntry(entry, entry.Name)

		if err != nil {
			return nil, err
		}
	}

	for len(entryPaths) != 0 {
		nextTree, err := headTree.Tree(entryPaths[0])

		if err != nil {
			return nil, err
		}

		for _, entry := range nextTree.Entries {
			nextPathName := path.Join(entryPaths[0], entry.Name)

			err = handleEntry(entry, nextPathName)

			if err != nil {
				return nil, err
			}
		}

		entryPaths = append([]string{}, entryPaths[1:]...)
	}

	// write end header
	tarWriter.Close()

	for _, hongKongBuildSetting := range hongKongSettings.BuildsInfo {
		if hongKongBuildSetting.Dockerfile == "" {
			return nil, errors.New("hong kong image settings missing dockerfile")
		}

		cancelCtx, cancelCancelCtx := context.WithCancel(context.Background())

		buildOptions := types.ImageBuildOptions{
			Tags: []string{
				fmt.Sprintf("docker.io/therileyjohnson/%s_%s:latest", githubRepo.Repository.Name, hongKongBuildSetting.Name),
				fmt.Sprintf("docker.io/therileyjohnson/%s_%s:%s", githubRepo.Repository.Name, hongKongBuildSetting.Name, githubRepo.After),
			},
			Dockerfile: hongKongBuildSetting.Dockerfile,
			// Squash:     true, // NEED EXPERIMENTAL MODE APPARENTLY
		}

		fmt.Println("ABOUT TO BUILD...")

		response, err := cli.ImageBuild(cancelCtx, &tarBuffer, buildOptions)

		if err != nil {
			cancelCancelCtx()

			return nil, err
		}

		fmt.Println("READING BUILD RESPONSE...")

		rb, err := ioutil.ReadAll(response.Body)

		fmt.Println("DONE READING BUILD RESPONSE...", string(rb))

		if err != nil {
			cancelCancelCtx()

			fmt.Println(string(rb))

			return nil, err
		}

		cancelCancelCtx()
	}

	return &hongKongSettings, nil
}

func main() {
	uyghursHost := flag.String("host", "therileyjohnson.com:8443", "http service address")

	development := flag.Bool("d", false, "development flag")
	envFile := flag.Bool("f", false, "use env file for config")

	flag.Parse()

	if *envFile {
		err := godotenv.Load()

		if err != nil {
			log.Fatal("Error loading .env file")
		}
	}

	envVars := make(map[string]string)

	for _, envVarKey := range []string{"DOCKERHUB_USERNAME", "DOCKERHUB_PASSWORD", "UYGHURS_PATH", "UYGHURS_KEY"} {
		envVarValue := os.Getenv(envVarKey)

		if envVarValue == "" {
			log.Fatalf(`environmental variable "%s" is not set`, envVarKey)
		}

		envVars[envVarKey] = strings.Trim(envVarValue, "\r\n")
	}

	dockerUsername := envVars["DOCKERHUB_USERNAME"]
	dockerPassword := envVars["DOCKERHUB_PASSWORD"]

	uyghursPath := envVars["UYGHURS_PATH"]
	uyghursKey := envVars["UYGHURS_KEY"]

	cli, err := client.NewEnvClient()

	if err != nil {
		log.Fatal(err)
	}

	uyghursScheme := "ws"

	if !*development {
		uyghursScheme = "wss"
	}

	log.Printf("~> Starting Hong-Kong Client, connection to %s://%s%s/UYGHURS_KEY \n", uyghursScheme, *uyghursHost, uyghursPath)

	interruptChan := make(chan os.Signal, 1)

	signal.Notify(interruptChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-interruptChan

		log.Fatalln("Killed by signal")
	}()

	uyghursURL := url.URL{Scheme: uyghursScheme, Host: *uyghursHost, Path: fmt.Sprintf("%s/%s", uyghursPath, uyghursKey)}

	// uyghursConnection, _, err := websocket.DefaultDialer.Dial(uyghursURL.String(), nil)

	// if err != nil {
	// 	log.Fatal("initial dial to uyghurs failed!")
	// } else {
	// 	defer uyghursConnection.Close()

	// 	log.Println("Connected to uyghurs server!")
	// }

	// reconnectIndefinitely := func() {
	// 	var reconnectErr error

	// 	for reconnectErr != nil {
	// 		if uyghursConnection != nil {
	// 			uyghursConnection.Close()
	// 		}

	// 		uyghursConnection, _, reconnectErr = websocket.DefaultDialer.Dial(uyghursURL.String(), nil)

	// 		log.Println("Reconnected to uyghurs server!")

	// 		time.Sleep(5 * time.Second)
	// 	}
	// }

	connectIndefinitely := func() net.Conn {
		var (
			conn    net.Conn
			connErr error
		)

		dialCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

		for conn, _, _, connErr = ws.DefaultDialer.Dial(dialCtx, uyghursURL.String()); connErr != nil; {
			time.Sleep(time.Second)

			cancel()

			dialCtx, cancel = context.WithTimeout(context.Background(), 5*time.Second)

			conn, _, _, connErr = ws.DefaultDialer.Dial(dialCtx, uyghursURL.String())
		}

		cancel()

		return conn
	}

	uyghursConnection := connectIndefinitely()

	log.Println("Initial connection to uyghurs")

	var workerMessage uyghurs.WorkerMessage

	receiveMessageChan := make(chan []byte)
	sendMessageChan := make(chan []byte)

	connectionLock := &sync.Mutex{}

	go func() {
		for {
			receiveMessageBytes, _, err := wsutil.ReadServerData(uyghursConnection)

			if err != nil {
				connectionLock.Lock()

				if uyghursConnection != nil {
					uyghursConnection.Close()
				}

				uyghursConnection = connectIndefinitely()

				log.Println("Reconnected to uyghurs server!")

				connectionLock.Unlock()

				continue
			}

			if len(receiveMessageBytes) != 0 {
				receiveMessageChan <- receiveMessageBytes
			}
		}
	}()

	go func() {
		var (
			messageSent      bool
			sendMessageBytes []byte
		)

		for sendMessageBytes = range sendMessageChan {
			messageSent = false

			for !messageSent {
				connectionLock.Lock()

				err = wsutil.WriteClientMessage(uyghursConnection, ws.OpText, sendMessageBytes)

				connectionLock.Unlock()

				if err != nil {
					log.Println("error sending work response JSON")

					continue
				}

				messageSent = true
			}

		}
	}()

	for messageBytes := range receiveMessageChan {
		// _, messageBytes, err := uyghursConnection.ReadMessage()

		// if err != nil {
		// 	if websocket.IsCloseError(err, websocket.CloseNoStatusReceived, websocket.CloseAbnormalClosure, websocket.CloseInternalServerErr) {
		// 		reconnectIndefinitely()

		// 		continue
		// 	} else if websocket.IsCloseError(err, websocket.CloseNoStatusReceived, websocket.CloseGoingAway, websocket.CloseMessage, websocket.CloseNormalClosure) {
		// 		log.Fatalln("Disconnected from uyghurs server,", err)
		// 	} else {
		// 		log.Fatalln("Disconnected from uyghurs server, unknown err,", err)
		// 	}
		// }

		err = json.Unmarshal(messageBytes, &workerMessage)

		if err != nil {
			log.Println("read error:", err)

			continue
		}

		switch uyghurs.WorkerMessageType(workerMessage.Type) {
		case uyghurs.WorkRequestType:
			var messageData uyghurs.WorkRequest

			err := mapstructure.Decode(workerMessage.MessageData, &messageData)

			sendResponse := func(sendErr error) {
				fmt.Println("error occured:", sendErr)

				sendResponseBytes, err := json.Marshal(uyghurs.WorkerMessage{
					Type: int(uyghurs.WorkResponseType),
					MessageData: structs.Map(uyghurs.WorkResponse{
						Err:             sendErr.Error(),
						GithubData:      messageData.GithubData,
						ProjectMetadata: uyghurs.ProjectMetadata{},
					}),
				})

				if err != nil {
					log.Println("error marshalling response JSON:", err)

					return
				}

				sendMessageChan <- sendResponseBytes

				// err = wsutil.WriteClientMessage(uyghursConnection, ws.OpText, sendResponseBytes)

				// if err != nil {
				// 	log.Println("error sending work response JSON:", err)
				// }
			}

			if err != nil {
				sendResponse(fmt.Errorf("error parsing worker work response: %s", err.Error()))

				continue
			}

			fmt.Println("Received WorkRequest")

			// fmt.Println(messageData)

			hongKongSettings, err := buildImageForApp(cli, messageData.GithubData)

			if err != nil {
				sendResponse(fmt.Errorf("error building image: %s", err.Error()))

				continue
			}

			hongKongSettings.ProjectName = messageData.GithubData.Repository.Name

			log.Printf("Image(s) built for %s\n", messageData.GithubData.Repository.Name)

			var pushErr error

			for _, hongKongBuildSetting := range hongKongSettings.BuildsInfo {
				timeoutContext, cancel := context.WithTimeout(context.Background(), 3*time.Minute)

				type pushResponse struct {
					err error
				}

				responseChan := make(chan pushResponse)

				authConfig := types.AuthConfig{
					Username: dockerUsername,
					Password: dockerPassword,
				}

				encodedJSON, err := json.Marshal(authConfig)

				if err != nil {
					cancel()

					panic(err)
				}

				go func() {
					response, err := cli.ImagePush(
						timeoutContext,
						fmt.Sprintf("docker.io/therileyjohnson/%s_%s:latest", messageData.GithubData.Repository.Name, hongKongBuildSetting.Name),
						types.ImagePushOptions{
							All:          true,
							RegistryAuth: base64.URLEncoding.EncodeToString(encodedJSON),
						},
					)

					if response != nil {
						// Need to wait for response to close for image to fully push
						io.Copy(os.Stdout, response)

						response.Close()
					}

					responseChan <- pushResponse{err}
				}()

				select {
				case <-timeoutContext.Done():
					pushErr = timeoutContext.Err()

					if pushErr != nil {
						log.Printf("pushing image timed out for %s\n", messageData.GithubData.Repository.Name)
					}
				case responseInfo := <-responseChan:
					pushErr = responseInfo.err
				}

				cancel()

				if pushErr != nil {
					sendResponse(pushErr)

					break
				}
			}

			if pushErr != nil {
				continue
			}

			// err = websocket.WriteJSON(uyghursConnection, uyghurs.WorkerMessage{
			// 	Type:        int(uyghurs.WorkResponseType),
			// 	MessageData: structs.Map(uyghurs.WorkResponse{"", messageData.GithubData, *hongKongSettings}),
			// })

			workResponseBytes, err := json.Marshal(uyghurs.WorkerMessage{
				Type:        int(uyghurs.WorkResponseType),
				MessageData: structs.Map(uyghurs.WorkResponse{"", messageData.GithubData, *hongKongSettings}),
			})

			if err != nil {
				log.Println("error marshalling work response JSON:", err)

				return
			}

			sendMessageChan <- workResponseBytes

			// err = wsutil.WriteClientMessage(uyghursConnection, ws.OpText, workResponseBytes)

			// if err != nil {
			// 	log.Printf("error sending work response JSON for %s: %s\n", messageData.GithubData.Repository.Name, err)
			// }

			log.Printf("pushed image successfully for %s\n", messageData.GithubData.Repository.Name)
		case uyghurs.PingRequestType:
			log.Println("Received PingRequest")
		default:
			log.Println("Unknown worker message type:", workerMessage.Type)

			continue
		}
	}
}
