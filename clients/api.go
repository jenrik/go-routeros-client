package clients

type Sentence map[string]string

type Response struct {
	Sentences []Sentence
}

type RouterOSClient interface {
	SendCommand(cmd string, args map[string]string) (error, Response)
}
