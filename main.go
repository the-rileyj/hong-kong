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
	"net/url"
	"os"
	"os/signal"
	"path"
	"syscall"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/fatih/structs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/gorilla/websocket"
	"github.com/mitchellh/mapstructure"
	"github.com/the-rileyj/uyghurs"
	"gopkg.in/yaml.v2"
)

func parseDockerComposeYML(dockerComposeBytes []byte) (uyghurs.HongKongImageSettings, error) {
	var hongKongSettings uyghurs.HongKongSettings

	err := yaml.Unmarshal(dockerComposeBytes, &hongKongSettings)

	if err != nil {
		return uyghurs.HongKongImageSettings{}, err
	}

	return hongKongSettings.HongKongImageSettings, nil
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

func buildImageForApp(cli *client.Client, githubRepo uyghurs.GithubPush) error {
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
		return err
	}

	headCommit, err := r.CommitObject(headCommitRef.Hash())

	if err != nil {
		return err
	}

	headTree, err := headCommit.Tree()

	if err != nil {
		return err
	}

	// tar up repo for docker image building
	var tarBuffer bytes.Buffer
	tarWriter := tar.NewWriter(&tarBuffer)

	entryPaths := make([]string, 0)

	var hongKongImageSettings uyghurs.HongKongImageSettings

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

				hongKongImageSettings, err = parseDockerComposeYML(dockerComposeBytes)

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
			return err
		}
	}

	for len(entryPaths) != 0 {
		nextTree, err := headTree.Tree(entryPaths[0])

		if err != nil {
			return err
		}

		for _, entry := range nextTree.Entries {
			nextPathName := path.Join(entryPaths[0], entry.Name)

			err = handleEntry(entry, nextPathName)

			if err != nil {
				return err
			}
		}

		entryPaths = append([]string{}, entryPaths[1:]...)
	}

	if hongKongImageSettings.Dockerfile == "" {
		return errors.New("hong kong image settings missing dockerfile")
	}

	// write end header
	tarWriter.Close()

	cancelCtx, cancelCancelCtx := context.WithCancel(context.Background())

	defer cancelCancelCtx()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		select {
		case <-c:
			cancelCancelCtx()

			<-cancelCtx.Done()
		}
	}()

	buildOptions := types.ImageBuildOptions{
		Tags: []string{
			fmt.Sprintf("docker.io/therileyjohnson/%s:latest", githubRepo.Repository.Name),
			fmt.Sprintf("docker.io/therileyjohnson/%s:%s", githubRepo.Repository.Name, githubRepo.After),
		},
		Dockerfile: hongKongImageSettings.Dockerfile,
		// Squash:     true, // NEED EXPERIMENTAL MODE APPARENTLY
	}

	response, err := cli.ImageBuild(cancelCtx, &tarBuffer, buildOptions)

	if err != nil {
		return err
	}

	rb, err := ioutil.ReadAll(response.Body)

	if err != nil {
		return err
	}

	fmt.Println(string(rb))

	return nil
}

func main() {
	envVars := make(map[string]string)

	for _, envVarKey := range []string{"DOCKER_USERNAME", "DOCKER_PASSWORD", "UYGHURS_KEY"} {
		envVarValue := os.Getenv(envVarKey)

		if envVarValue == "" {
			log.Fatalf(`environmental variable "%s" is not set`, envVarKey)
		}

		envVars[envVarKey] = envVarValue
	}

	dockerUsername := envVars["DOCKER_USERNAME"]
	dockerPassword := envVars["DOCKER_PASSWORD"]

	uyghursKey := envVars["UYGHURS_KEY"]

	cli, err := client.NewEnvClient()

	if err != nil {
		panic(err)
	}

	uyghursHost := flag.String("host", "localhost", "http service address")

	development := flag.Bool("d", false, "development flag")

	flag.Parse()

	uyghursHostname := fmt.Sprintf("%s:9969", *uyghursHost)

	uyghursScheme := "ws"

	if *development {
		uyghursScheme = "wss"
	}

	log.Printf("~> Starting Hong-Kong Client, connection to %s://%s/worker/UYGHURS_KEY \n", uyghursScheme, uyghursHostname)

	interruptChan := make(chan os.Signal, 1)

	signal.Notify(interruptChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-interruptChan

		log.Fatalln("Killed by signal")
	}()

	uyghursURL := url.URL{Scheme: uyghursScheme, Host: uyghursHostname, Path: fmt.Sprintf("/worker/%s", uyghursKey)}

	uyghursConnection, _, err := websocket.DefaultDialer.Dial(uyghursURL.String(), nil)

	if err != nil {
		log.Fatal("dial to uyghurs failed!")
	}

	defer uyghursConnection.Close()

	log.Println("Connected to uyghurs server!")

	var workerMessage uyghurs.WorkerMessage

	for {
		_, messageBytes, err := uyghursConnection.ReadMessage()

		if websocket.IsCloseError(err, websocket.CloseNoStatusReceived, websocket.CloseAbnormalClosure, websocket.CloseInternalServerErr) {
			for err != nil {
				time.Sleep(5 * time.Second)

				uyghursConnection.Close()

				uyghursConnection, _, err = websocket.DefaultDialer.Dial(uyghursURL.String(), nil)

				defer uyghursConnection.Close()
			}

			log.Println("Reconnected to uyghurs server!")

			continue
		} else if websocket.IsCloseError(err, websocket.CloseNoStatusReceived, websocket.CloseGoingAway, websocket.CloseMessage, websocket.CloseNormalClosure) {
			log.Fatalln("Disconnected from uyghurs server,", err)
		} else {
			log.Fatalln("Disconnected from uyghurs server, unknown err,", err)
		}

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

				err := websocket.WriteJSON(uyghursConnection, uyghurs.WorkerMessage{
					Type:        int(uyghurs.WorkResponseType),
					MessageData: structs.Map(uyghurs.WorkResponse{sendErr.Error(), messageData.GithubData}),
				})

				if err != nil {
					log.Println("error sending work response JSON:", err)
				}
			}

			if err != nil {
				sendResponse(fmt.Errorf("error parsing worker work response: %s", err.Error()))

				continue
			}

			fmt.Println("Received WorkRequest")

			fmt.Println(messageData)

			err = buildImageForApp(cli, messageData.GithubData)

			if err != nil {
				sendResponse(fmt.Errorf("error building image: %s", err.Error()))

				continue
			}

			log.Printf("Image built for %s\n", messageData.GithubData.Repository.Name)

			timeoutContext, cancel := context.WithTimeout(context.Background(), time.Minute)

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
				panic(err)
			}

			go func() {
				response, err := cli.ImagePush(
					timeoutContext,
					fmt.Sprintf("docker.io/therileyjohnson/%s:latest", messageData.GithubData.Repository.Name),
					types.ImagePushOptions{
						All:          true,
						RegistryAuth: base64.URLEncoding.EncodeToString(encodedJSON),
					},
				)

				if response != nil {
					// Need to wait for response to close for image to fully push
					io.Copy(ioutil.Discard, response)

					response.Close()
				}

				responseChan <- pushResponse{err}
			}()

			var pushErr error

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

				continue
			}

			err = websocket.WriteJSON(uyghursConnection, uyghurs.WorkerMessage{
				Type:        int(uyghurs.WorkResponseType),
				MessageData: structs.Map(uyghurs.WorkResponse{"", messageData.GithubData}),
			})

			if err != nil {
				log.Printf("error sending work response JSON for %s: %s\n", messageData.GithubData.Repository.Name, err)
			}

			log.Printf("pushed image successfully for %s\n", messageData.GithubData.Repository.Name)
		case uyghurs.PingRequestType:
			log.Println("Received PingRequest")
		default:
			log.Println("Unknown worker message type:", workerMessage.Type)

			continue
		}
	}
}
