package fluent

import (
	"github.com/jenrik/go-routeros-client/clients"
	"github.com/jenrik/go-routeros-client/clients/rest"
	"net/url"
)

type Client struct {
	client clients.RouterOSClient
}

func New(client clients.RouterOSClient) *Client {
	return &Client{
		client: client,
	}
}

func (client *Client) root() root {
	return root{
		client: client,
	}
}

func foo() {
	u, err := url.Parse("http://localhost:8080/")
	if err != nil {
		panic(err)
	}
	restClient := rest.New(*u, nil)
	client := New(restClient)
	err, _ = client.root().
		cat_ip().
		cat_arp().
		cmd_add(
			"",
			"",
			"",
			"",
			"",
			"",
		)
	if err != nil {
		panic(err)
	}
}
