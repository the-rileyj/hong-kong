package uyghurs

/*
{
  "ref": "refs/heads/master",
  "repository": {
    "name": "uyghurs",
    "url": "https://github.com/the-rileyj/uyghurs",
    "created_at": 1595113171,
    "updated_at": "2020-07-19T00:15:43Z",
    "pushed_at": 1595118640,
    "git_url": "git://github.com/the-rileyj/uyghurs.git",
    "ssh_url": "git@github.com:the-rileyj/uyghurs.git",
    "default_branch": "master",
    "master_branch": "master"
  }
}
*/

type UyghursSecrets struct {
	UyghursKey string `json:"uyghursKey"`
}

type HongKongSettings struct {
	HongKongProjectSettings ProjectMetadata `yaml:"x-hong-kong"`
}

type GithubPush struct {
	Ref        string     `json:"ref"`
	After      string     `json:"after"`
	Repository Repository `json:"repository"`
}

type Repository struct {
	Name          string `json:"name"`
	URL           string `json:"url"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
	PushedAt      int64  `json:"pushed_at"`
	GitURL        string `json:"git_url"`
	SSHURL        string `json:"ssh_url"`
	DefaultBranch string `json:"default_branch"`
	MasterBranch  string `json:"master_branch"`
}

type WorkerMessage struct {
	Type        int                    `json:"type"`
	MessageData map[string]interface{} `json:"messageData"`
}

type WorkerMessageType int

const (
	WorkRequestType WorkerMessageType = iota
	WorkResponseType
	PingRequestType
	PingResponseType
)

type WorkerStateType int

const (
	Idle WorkerStateType = iota
	Building
)

type WorkRequest struct {
	GithubData GithubPush `json:"githubData"`
}

type WorkResponse struct {
	Err             string
	GithubData      GithubPush      `json:"githubData"`
	ProjectMetadata ProjectMetadata `json:"projectMetaData"`
}

type PingResponse struct {
	State WorkerMessageType `json:"state"`
}

type ProjectMetadata struct {
	ProjectName   string       `json:"projectName" yaml:"projectName"`
	BuildsInfo    []*BuildInfo `json:"buildInfo" yaml:"buildInfo"`
	ProjectRoutes []*RouteInfo `json:"projectRoutes" yaml:"projectRoutes"`
}

type BuildInfo struct {
	Context    string `json:"context" yaml:"context"`
	Dockerfile string `json:"dockerfile" yaml:"dockerfile"`
	Name       string `json:"name" yaml:"name"`
}

type RouteInfo struct {
	ForwardHost string `json:"forwardHost" yaml:"forwardHost"`
	Route       string `json:"route" yaml:"route"`
	Domain      string `json:"domain" yaml:"domain"`
}
