package tcp

import (
	"github.com/jenrik/go-routeros-client/clients"
	"net"
	"sync/atomic"
)

type cmd struct {
	cmd      string
	args     map[string]string
	respChan chan<- envelope
}

type envelope struct {
	err  error
	resp clients.Response
}

type RouterOSTCPClient struct {
	running   *atomic.Bool
	address   net.TCPAddr
	cmdChan   chan<- cmd
	closeChan chan<- struct{}
}

// TODO support cancellation via "/cancal"
// TODO support ".../listen" and its continuous output

func (client *RouterOSTCPClient) SendCommand(command string, args map[string]string) (error, clients.Response) {
	respChan := make(chan envelope)
	cmd := cmd{
		cmd:      command,
		args:     args,
		respChan: respChan,
	}

	client.cmdChan <- cmd

	resp := <-respChan
	return resp.err, resp.resp
}

func New(addr net.TCPAddr, username string, password string) (*RouterOSTCPClient, error) {
	running := atomic.Bool{}
	running.Store(true)

	cmdChan := make(chan cmd)
	closeChan := make(chan struct{})

	err := startWorker(addr, cmdChan, closeChan)
	if err != nil {
		return nil, err
	}

	client := &RouterOSTCPClient{
		running:   &running,
		address:   addr,
		cmdChan:   cmdChan,
		closeChan: closeChan,
	}

	err, _ = client.SendCommand("/login", map[string]string{"=name": username, "=password": password})
	if err != nil {
		return client, err
	}

	return client, nil
}

func (client *RouterOSTCPClient) Close() {
	if client.running.Load() {
		client.closeChan <- struct{}{}
		client.running.Store(false)
	}
}
