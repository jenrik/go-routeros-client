package api

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"github.com/jenrik/go-routeros-client/clients"
	"io"
	"net"
	"regexp"
	"strconv"
)

type replyAccumulator struct {
	sentences []clients.Sentence
	replyChan chan<- envelope
	trap      *RouterOSTCPErrorReply
}

type worker struct {
	conn       *net.TCPConn
	reader     io.Reader
	writer     *bufio.Writer
	cmdChan    <-chan cmd
	counter    Tag
	replyChans map[Tag]*replyAccumulator
	closeChan  <-chan struct{}
}

type RouterOSTCPErrorReply struct {
	ReceivedSentences []clients.Sentence
	Words             clients.Sentence
}

func (err RouterOSTCPErrorReply) Error() string {
	bytes, marshallErr := json.Marshal(err.Words)
	if marshallErr != nil {
		panic(err)
	}
	return "RouterOS send error or exception in response to command, " + string(bytes)
}

type RouterOSTCPFatalError struct {
	Words clients.Sentence
}

func (err RouterOSTCPFatalError) Error() string {
	return "RouterOS send sentence indicating fatal error"
}

type RouterOSTCPErrorUnknownControlByte struct {
	ControlByte byte
}

func (err RouterOSTCPErrorUnknownControlByte) Error() string {
	return "Unsupported control byte received from RouterOS"
}

type RouterOSTCPError struct {
	msg string
	err error
}

func (err RouterOSTCPError) Error() string {
	return err.msg
}

func (err RouterOSTCPError) Unwrap() error {
	return err.err
}

func wrapError(msg string, err error) RouterOSTCPError {
	return RouterOSTCPError{
		msg: msg,
		err: err,
	}
}

func genericError(msg string) RouterOSTCPError {
	return RouterOSTCPError{
		msg: msg,
		err: nil,
	}
}

func startWorker(addr net.TCPAddr, cmdChan <-chan cmd, closeChan <-chan struct{}) error {
	// TODO add support for secure connection
	conn, err := net.DialTCP("tcp", nil, &addr)
	if err != nil {
		return err
	}

	worker := worker{
		conn:       conn,
		writer:     bufio.NewWriter(conn),
		reader:     bufio.NewReader(conn),
		cmdChan:    cmdChan,
		closeChan:  closeChan,
		replyChans: map[Tag]*replyAccumulator{},
		counter:    0,
	}
	go worker.startPump()

	return nil
}

func (worker *worker) startReadPump(shutdownChan <-chan struct{}) (<-chan []string, <-chan error) {
	sentenceChan := make(chan []string, 8)
	errChan := make(chan error, 1)

	go func() {
		lengthBuffer := make([]byte, 5)
		sentence := make([]string, 0)
		for {
			select {
			case <-shutdownChan:
				return
			default:
			}

			// Read word
			bytesRead, err := worker.reader.Read(lengthBuffer[0:1])
			if err != nil {
				errChan <- wrapError("Error while reading first length byte of word", err)
				return
			}
			if bytesRead < 1 {
				// TODO better error handling
				panic("not enough bytes read")
			}

			if lengthBuffer[0] >= 0xF8 {
				// Control byte
				errChan <- RouterOSTCPErrorUnknownControlByte{
					ControlByte: lengthBuffer[0],
				}
				return
			}
			lengthBytes := decodeLengthByte(lengthBuffer[0])

			if lengthBytes > 1 {
				bytesRead, err := worker.reader.Read(lengthBuffer[1:lengthBytes])
				if err != nil {
					errChan <- wrapError("Error while reading first length byte of word", err)
					return
				}
				if bytesRead < (lengthBytes - 1) {
					errChan <- genericError("Failed to read enough length bytes")
					return
				}
			}

			length := decodeLength(lengthBuffer[0:lengthBytes])

			if length == 0 {
				sentenceChan <- sentence
				// Reset word accumulator; start new sentence
				sentence = make([]string, 0)
				continue
			}

			word := make([]byte, length)
			read := uint32(0)
			for {
				bytesRead, err := worker.reader.Read(word[read:])
				if err != nil {
					errChan <- err
					return
				}
				read += uint32(bytesRead)

				if read >= length {
					break
				}
			}

			sentence = append(sentence, string(word))
		}
	}()

	return sentenceChan, errChan
}

func decodeLengthByte(b byte) int {
	if (b & 0x80) == 0 {
		return 1
	} else if (b & 0xF0) == 0xE0 {
		return 4
	} else if (b & 0xE0) == 0xC0 {
		return 3
	} else if (b & 0xC0) == 0x80 {
		return 2
	} else if b == 0xF0 {
		return 5
	} else {
		panic("invalid length byte")
	}
}

func decodeLength(bytes []byte) uint32 {
	switch len(bytes) {
	case 1:
		return binary.BigEndian.Uint32([]byte{0, 0, 0, bytes[0] & 0x7F})
	case 2:
		return binary.BigEndian.Uint32([]byte{0, 0, bytes[0] & 0x7F, bytes[1]})
	case 3:
		return binary.BigEndian.Uint32([]byte{0, bytes[0] & 0x3F, bytes[1], bytes[2]})
	case 4:
		return binary.BigEndian.Uint32([]byte{bytes[0] & 0x1F, bytes[1], bytes[2], bytes[3]})
	case 5:
		return binary.BigEndian.Uint32(bytes[1:5])
	default:
		panic("Invalid state, more than 5 length bytes")
	}
}

func (worker *worker) startPump() {
	readerShutdownChan := make(chan struct{})
	sentenceChan, readerErrChan := worker.startReadPump(readerShutdownChan)

	for {
		select {
		case <-worker.closeChan:
			readerShutdownChan <- struct{}{}
			return
		case cmd := <-worker.cmdChan:
			bytes, tag := worker.encodeCommand(cmd)

			// Register reply handler
			worker.replyChans[tag] = &replyAccumulator{
				replyChan: cmd.respChan,
			}

			// Send command
			_, err := worker.writer.Write(bytes)
			if err != nil {
				worker.broadcastError(err)
				closeErr := worker.conn.Close()
				if closeErr != nil {
					panic(closeErr)
				}
				readerShutdownChan <- struct{}{}
				return
			}

			// Flush command
			err = worker.writer.Flush()
			if err != nil {
				worker.broadcastError(err)
				closeErr := worker.conn.Close()
				if closeErr != nil {
					panic(closeErr)
				}
				readerShutdownChan <- struct{}{}
				return
			}
		case reply := <-sentenceChan:
			switch reply[0] {
			case "!done":
				// no more sentences will be sent for that tag
				attrs, err := splitAttributes(reply[1:])
				if err != nil {
					worker.broadcastError(err)
					closeErr := worker.conn.Close()
					if closeErr != nil {
						panic(closeErr)
					}
					readerShutdownChan <- struct{}{}
					return
				}

				tag, err := getTag(attrs)
				if err != nil {
					worker.broadcastError(err)
					closeErr := worker.conn.Close()
					if closeErr != nil {
						panic(closeErr)
					}
					readerShutdownChan <- struct{}{}
					return
				}

				delete(attrs, ".tag")

				sentences := append(worker.replyChans[tag].sentences, attrs)

				env := envelope{
					resp: clients.Response{
						Sentences: sentences,
					},
				}
				if worker.replyChans[tag].trap != nil {
					env.err = *worker.replyChans[tag].trap
				}
				worker.replyChans[tag].replyChan <- env

				// Remove reply handler because we have completed the command
				delete(worker.replyChans, tag)
			case "!re":
				// more sentences will be sent for that tag
				attrs, err := splitAttributes(reply[1:])
				if err != nil {
					worker.broadcastError(err)
					closeErr := worker.conn.Close()
					if closeErr != nil {
						panic(closeErr)
					}
					readerShutdownChan <- struct{}{}
					return
				}

				tag, err := getTag(attrs)
				if err != nil {
					worker.broadcastError(err)
					closeErr := worker.conn.Close()
					if closeErr != nil {
						panic(closeErr)
					}
					readerShutdownChan <- struct{}{}
					return
				}

				delete(attrs, ".tag")

				// Accumulate reply sentence
				worker.replyChans[tag].sentences = append(worker.replyChans[tag].sentences, attrs)
			case "!trap":
				attrs, err := splitAttributes(reply[1:])
				if err != nil {
					worker.broadcastError(err)
					closeErr := worker.conn.Close()
					if closeErr != nil {
						panic(closeErr)
					}
					readerShutdownChan <- struct{}{}
					return
				}

				tag, err := getTag(attrs)
				if err != nil {
					worker.broadcastError(err)
					closeErr := worker.conn.Close()
					if closeErr != nil {
						panic(closeErr)
					}
					readerShutdownChan <- struct{}{}
					return
				}

				delete(attrs, ".tag")

				sentences := append(worker.replyChans[tag].sentences, attrs)

				worker.replyChans[tag].trap = &RouterOSTCPErrorReply{
					ReceivedSentences: sentences,
					Words:             attrs,
				}
			case "!fatal":
				// fatal, end of connection
				worker.broadcastError(RouterOSTCPFatalError{})
				closeErr := worker.conn.Close()
				if closeErr != nil {
					panic(closeErr)
				}
				readerShutdownChan <- struct{}{}
				return
			}
		case err := <-readerErrChan:
			worker.broadcastError(err)
			closeErr := worker.conn.Close()
			if closeErr != nil {
				panic(closeErr)
			}
			return
		}
	}
}

var attributeSplitRegex = regexp.MustCompile("^=?(?P<key>[^=]+)=(?P<value>.*)$")
var attributeKeyIndex = attributeSplitRegex.SubexpIndex("key")
var attributeValueIndex = attributeSplitRegex.SubexpIndex("value")

func splitAttributes(attributeWords []string) (map[string]string, error) {
	attrs := map[string]string{}
	for _, word := range attributeWords {
		groups := attributeSplitRegex.FindStringSubmatch(word)
		if groups == nil {
			return nil, errors.New("malformed word, unable to split")
		}

		attrs[groups[attributeKeyIndex]] = groups[attributeValueIndex]
	}

	return attrs, nil
}

func getTag(attrs map[string]string) (Tag, error) {
	rawTag, ok := attrs[".tag"]
	if !ok {
		return -1, errors.New("Missing tag")
	}

	tag, err := strconv.Atoi(rawTag)
	if err != nil {
		return -1, err
	}

	return Tag(tag), nil
}

func encodeLength(length uint32) []byte {
	if length <= 0x7F {
		bytes := make([]byte, 4)
		binary.BigEndian.PutUint32(bytes, length)
		return bytes[3:4]
	} else if length <= 0x3FFF {
		bytes := make([]byte, 4)
		binary.BigEndian.PutUint32(bytes, length)
		return []byte{bytes[2] | 0x80, bytes[3]}
	} else if length <= 0x1FFFFF {
		bytes := make([]byte, 4)
		binary.BigEndian.PutUint32(bytes, length)
		return []byte{bytes[1] | 0xC0, bytes[2], bytes[3]}
	} else if length <= 0xFFFFFFF {
		bytes := make([]byte, 4)
		binary.BigEndian.PutUint32(bytes, length)
		return []byte{bytes[0] | 0xE0, bytes[1], bytes[2], bytes[3]}
	} else {
		bytes := make([]byte, 4)
		binary.BigEndian.PutUint32(bytes, length)
		return append([]byte{0xF0}, bytes...)
	}
}

type Tag int

func (worker *worker) encodeCommand(cmd cmd) ([]byte, Tag) {
	words := make([][]byte, 1)
	words[0] = encodeWord(cmd.cmd)
	for key, value := range cmd.args {
		words = append(words, encodeWord(key+"="+value))
	}

	// Add tag for tracking purposes
	tag := worker.counter
	worker.counter += 1
	words = append(words, encodeWord(".tag="+strconv.Itoa(int(tag))))

	// Zero length word to terminate sentence
	words = append(words, encodeLength(0))

	var bytes []byte
	for _, word := range words {
		bytes = append(bytes, word...)
	}

	return bytes, tag
}

func encodeWord(word string) []byte {
	kindBytes := []byte(word)
	return append(encodeLength(uint32(len(kindBytes))), kindBytes...)
}

func (worker *worker) broadcastError(err error) {
	for _, acc := range worker.replyChans {
		acc.replyChan <- envelope{
			err: err,
		}
	}
}
