package rest

import (
	"encoding/json"
	"github.com/jenrik/go-routeros-client/clients"
	"net/http"
	"net/url"
	"strconv"
)

type RouterOSRestClient struct {
	addr     *url.URL
	client   *http.Client
	username string
	password string
}

type RouteOSRestError struct {
	msg string
	err error
}

func (err RouteOSRestError) Error() string {
	if err.err != nil {
		return err.msg + ": " + err.err.Error()
	} else {
		return err.msg
	}
}

func (err RouteOSRestError) Unwrap() error {
	return err.err
}

type RouteOSRestErrorResponse struct {
	Err     int    `json:"error"`
	Message string `json:"message"`
	Detail  string `json:"detail"`
}

func (err RouteOSRestErrorResponse) Error() string {
	return err.Detail
}

func (client *RouterOSRestClient) SendCommand(cmd string, args map[string]string) (error, clients.Response) {
	addr := client.addr
	path, err := url.JoinPath(addr.Path, cmd)
	if err != nil {
		return err, clients.Response{}
	}
	addr.Path = path
	query := addr.Query()
	for key, value := range args {
		query.Add(key, value)
	}
	addr.User = url.UserPassword(client.username, client.password)
	req := &http.Request{
		URL:    addr,
		Method: http.MethodPost,
	}

	resp, err := client.client.Do(req)
	if err != nil {
		return err, clients.Response{}
	}

	if resp.StatusCode != http.StatusOK {
		return RouteOSRestError{msg: "Non-200 error code: " + strconv.Itoa(resp.StatusCode)}, clients.Response{}
	}

	bodyDecoder := json.NewDecoder(resp.Body)
	rawJson := &json.RawMessage{}
	err = bodyDecoder.Decode(rawJson)
	if err != nil {
		return RouteOSRestError{msg: "Failed to parse command JSON response", err: err}, clients.Response{}
	}

	var rawData interface{}
	err = json.Unmarshal(*rawJson, &rawData)
	if err != nil {
		// TODO better error
		return err, clients.Response{}
	}
	switch rawData.(type) {
	case map[string]interface{}:
		single := rawData.(map[string]interface{})
		// Error response
		if _, ok := single["error"]; ok {
			var errResp RouteOSRestErrorResponse
			err = json.Unmarshal(*rawJson, &errResp)
			if err != nil {
				return RouteOSRestError{
					msg: "Failed to unmarshall error response",
					err: err,
				}, clients.Response{}
			}

			return errResp, clients.Response{}
		}

		words := map[string]string{}
		for key, value := range single {
			words[key] = value.(string)
		}
		return nil, clients.Response{
			Sentences: []clients.Sentence{words},
		}
	case []interface{}:
		// multi sentence response
		var sentences []clients.Sentence
		for _, line := range rawData.([]interface{}) {
			words := map[string]string{}
			for key, value := range line.(map[string]interface{}) {
				words[key] = value.(string)
			}
			sentences = append(sentences, words)
		}

		return nil, clients.Response{
			Sentences: sentences,
		}
	default:
		return RouteOSRestError{msg: "Unexpected response data"}, clients.Response{}
	}
}

func New(addr *url.URL, username string, password string, client *http.Client) *RouterOSRestClient {
	if client == nil {
		client = http.DefaultClient
	}
	return &RouterOSRestClient{
		addr:     addr,
		client:   client,
		username: username,
		password: password,
	}
}
