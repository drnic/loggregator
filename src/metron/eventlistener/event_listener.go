package eventlistener

import (
	"github.com/cloudfoundry/gosteno"
	"github.com/cloudfoundry/loggregatorlib/cfcomponent/instrumentation"
	"net"
	"sync"
	"sync/atomic"
)

type EventListener interface {
	instrumentation.Instrumentable
	Start()
	Stop()
}

type heartbeatRequester interface {
	Start(net.Addr, net.PacketConn)
}

type eventListener struct {
	host        string
	dataChannel chan []byte
	connection  net.PacketConn
	requester   heartbeatRequester

	receivedMessageCount uint64
	receivedByteCount    uint64
	contextName          string

	sync.RWMutex
	*gosteno.Logger
}

func NewEventListener(host string, givenLogger *gosteno.Logger, name string, requester heartbeatRequester) (EventListener, <-chan []byte) {
	byteChan := make(chan []byte, 1024)
	return &eventListener{Logger: givenLogger, host: host, dataChannel: byteChan, contextName: name, requester: requester}, byteChan
}

func (eventListener *eventListener) Start() {
	connection, err := net.ListenPacket("udp", eventListener.host)
	if err != nil {
		eventListener.Fatalf("Failed to listen on port. %s", err)
	}
	eventListener.Infof("Listening on port %s", eventListener.host)
	eventListener.Lock()
	eventListener.connection = connection
	eventListener.Unlock()

	readBuffer := make([]byte, 65535) //buffer with size = max theoretical UDP size
	defer close(eventListener.dataChannel)
	for {
		readCount, senderAddr, err := connection.ReadFrom(readBuffer)
		if err != nil {
			eventListener.Debugf("Error while reading. %s", err)
			return
		}
		eventListener.Debugf("EventListener: Read %d bytes from address %s", readCount, senderAddr)
		readData := make([]byte, readCount) //pass on buffer in size only of read data
		copy(readData, readBuffer[:readCount])

		atomic.AddUint64(&eventListener.receivedMessageCount, 1)
		atomic.AddUint64(&eventListener.receivedByteCount, uint64(readCount))
		eventListener.dataChannel <- readData

		go eventListener.requester.Start(senderAddr, connection)
	}
}

func (eventListener *eventListener) Stop() {
	eventListener.Lock()
	defer eventListener.Unlock()
	eventListener.connection.Close()
}

func (eventListener *eventListener) metrics() []instrumentation.Metric {
	return []instrumentation.Metric{
		instrumentation.Metric{Name: "currentBufferCount", Value: len(eventListener.dataChannel)},
		instrumentation.Metric{Name: "receivedMessageCount", Value: atomic.LoadUint64(&eventListener.receivedMessageCount)},
		instrumentation.Metric{Name: "receivedByteCount", Value: atomic.LoadUint64(&eventListener.receivedByteCount)},
	}
}

func (eventListener *eventListener) Emit() instrumentation.Context {
	return instrumentation.Context{Name: eventListener.contextName,
		Metrics: eventListener.metrics(),
	}
}
