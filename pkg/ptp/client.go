package ptp

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

var tempPathCounter int64

// Client represents a connection to ptp4l via UDS socket
type Client struct {
	conn       net.Conn
	udsPath    string
	domain     uint8
	clockID    ClockIdentity
	portNum    uint16
	seqID      uint16
	mu         sync.Mutex
	targetAddr *net.UnixAddr
	tempPath   string
	verbose    bool
}

// Monitor provides monitoring functionality for ptp4l
type Monitor struct {
	client              *Client
	interval            time.Duration
	subscription        *SubscriptionManager
	portStates          map[uint16]uint8 // track port states for change detection
	subscriptionTimeout time.Duration    // timeout for subscription requests
	mu                  sync.RWMutex
}

// NewClient creates a new ptp4l client
func NewClient(udsPath string, domain uint8, verbose bool) (*Client, error) {
	client := &Client{
		udsPath: udsPath,
		domain:  domain,
		seqID:   1,
		verbose: verbose,
	}

	// Generate clock identity and port number like PMC does for UDS transport
	procID := uint32(os.Getpid())
	client.clockID = ClockIdentity{
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		uint8((procID & 0xFF000000) >> 24), // byte 6
		uint8((procID & 0x00FF0000) >> 16), // byte 7
	}
	client.portNum = uint16(procID & 0xFFFF)

	// Connect to ptp4l
	if err := client.connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to ptp4l: %v", err)
	}

	return client, nil
}

// connect establishes connection to ptp4l UDS socket
func (c *Client) connect() error {
	// Create unconnected Unix datagram socket like PMC
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_DGRAM, 0)
	if err != nil {
		return fmt.Errorf("failed to create socket: %v", err)
	}

	// Create temporary path for binding (like PMC does)
	tempPath := fmt.Sprintf("/var/run/pmc-go.%d.%d", os.Getpid(), atomic.AddInt64(&tempPathCounter, 1))

	// Bind to temporary path
	bindAddr := &syscall.SockaddrUnix{Name: tempPath}
	if err := syscall.Bind(fd, bindAddr); err != nil {
		syscall.Close(fd)
		return fmt.Errorf("failed to bind to %s: %v", tempPath, err)
	}

	// Convert to Go net.Conn
	file := os.NewFile(uintptr(fd), "unix-socket")
	conn, err := net.FileConn(file)
	if err != nil {
		file.Close()
		syscall.Close(fd)
		return fmt.Errorf("failed to create conn from file: %v", err)
	}

	// Resolve target address
	addr, err := net.ResolveUnixAddr("unixgram", c.udsPath)
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to resolve target address: %v", err)
	}

	c.conn = conn
	c.targetAddr = addr
	c.tempPath = tempPath

	log.Printf("Created unconnected Unix datagram socket bound to %s", tempPath)
	return nil
}

// Close closes the connection to ptp4l
func (c *Client) Close() error {
	if c.conn != nil {
		c.conn.Close()
	}
	// Clean up temporary socket path
	if c.tempPath != "" {
		os.Remove(c.tempPath)
	}
	return nil
}

// GetDefaultDataSet retrieves the default data set from ptp4l
func (c *Client) GetDefaultDataSet() (*DefaultDataSet, error) {
	resp, err := c.sendPMCStyleRequest(MID_DEFAULT_DATA_SET, 0)
	if err != nil {
		return nil, err
	}

	if resp.TLV == nil || len(resp.TLV.Data) < 20 {
		return nil, fmt.Errorf("invalid DEFAULT_DATA_SET response")
	}

	data := resp.TLV.Data
	dds := &DefaultDataSet{}

	// Parse flags and other fields according to IEEE 1588 format
	flags := binary.BigEndian.Uint16(data[0:2])
	dds.TwoStepFlag = (flags & 0x0200) != 0
	dds.SlaveOnly = (flags & 0x0100) != 0

	dds.NumberPorts = binary.BigEndian.Uint16(data[2:4])
	dds.Priority1 = data[4]

	dds.ClockQuality.ClockClass = data[5]
	dds.ClockQuality.ClockAccuracy = data[6]
	dds.ClockQuality.OffsetScaledLogVariance = binary.BigEndian.Uint16(data[7:9])

	dds.Priority2 = data[9]
	copy(dds.ClockIdentity[:], data[10:18])
	dds.DomainNumber = data[18]

	return dds, nil
}

// GetCurrentDataSet gets the current data set
func (c *Client) GetCurrentDataSet() (*CurrentDataSet, error) {
	resp, err := c.sendPMCStyleRequest(MID_CURRENT_DATA_SET, 0)
	if err != nil {
		return nil, err
	}

	if resp.TLV == nil || len(resp.TLV.Data) < 18 {
		return nil, fmt.Errorf("invalid CURRENT_DATA_SET response")
	}

	data := resp.TLV.Data
	cds := &CurrentDataSet{
		StepsRemoved:     binary.BigEndian.Uint16(data[0:2]),
		OffsetFromMaster: int64(binary.BigEndian.Uint64(data[2:10])),
		MeanPathDelay:    int64(binary.BigEndian.Uint64(data[10:18])),
	}

	return cds, nil
}

// GetParentDataSet gets the parent data set
func (c *Client) GetParentDataSet() (*ParentDataSet, error) {
	resp, err := c.sendPMCStyleRequest(MID_PARENT_DATA_SET, 0)
	if err != nil {
		return nil, err
	}

	if resp.TLV == nil || len(resp.TLV.Data) < 32 {
		return nil, fmt.Errorf("invalid PARENT_DATA_SET response")
	}

	data := resp.TLV.Data
	pds := &ParentDataSet{
		ParentStats:                           (data[10] & 0x01) != 0,
		ObservedParentOffsetScaledLogVariance: binary.BigEndian.Uint16(data[12:14]),
		ObservedParentClockPhaseChangeRate:    int32(binary.BigEndian.Uint32(data[14:18])),
		GrandmasterPriority1:                  data[18],
		GrandmasterPriority2:                  data[23],
	}

	// Parse ParentPortIdentity
	copy(pds.ParentPortIdentity.ClockIdentity[:], data[0:8])
	pds.ParentPortIdentity.PortNumber = binary.BigEndian.Uint16(data[8:10])

	// Parse GrandmasterIdentity
	copy(pds.GrandmasterIdentity[:], data[24:32])

	// Parse GrandmasterClockQuality
	pds.GrandmasterClockQuality = ClockQuality{
		ClockClass:              data[19],
		ClockAccuracy:           data[20],
		OffsetScaledLogVariance: binary.BigEndian.Uint16(data[21:23]),
	}

	return pds, nil
}

// GetGrandmasterSettingsNP gets the grandmaster settings (non-standard)
func (c *Client) GetGrandmasterSettingsNP() (*GrandmasterSettingsNP, error) {
	resp, err := c.sendPMCStyleRequest(MID_GRANDMASTER_SETTINGS_NP, 0)
	if err != nil {
		return nil, err
	}

	if resp.TLV == nil || len(resp.TLV.Data) < 8 {
		return nil, fmt.Errorf("invalid GRANDMASTER_SETTINGS_NP response")
	}

	data := resp.TLV.Data
	gs := &GrandmasterSettingsNP{
		ClockQuality: ClockQuality{
			ClockClass:              data[0],
			ClockAccuracy:           data[1],
			OffsetScaledLogVariance: binary.BigEndian.Uint16(data[2:4]),
		},
		UtcOffset:  int16(binary.BigEndian.Uint16(data[4:6])),
		TimeFlags:  data[6],
		TimeSource: data[7],
	}

	return gs, nil
}

// GetExternalGrandmasterPropertiesNP gets the external grandmaster properties (non-standard)
func (c *Client) GetExternalGrandmasterPropertiesNP() (*ExternalGrandmasterPropertiesNP, error) {
	resp, err := c.sendPMCStyleRequest(MID_EXTERNAL_GRANDMASTER_PROPERTIES_NP, 0)
	if err != nil {
		return nil, err
	}

	if resp.TLV == nil || len(resp.TLV.Data) < 10 {
		return nil, fmt.Errorf("invalid EXTERNAL_GRANDMASTER_PROPERTIES_NP response")
	}

	data := resp.TLV.Data
	egp := &ExternalGrandmasterPropertiesNP{
		StepsRemoved: binary.BigEndian.Uint16(data[8:10]),
	}

	// Parse GmIdentity
	copy(egp.GmIdentity[:], data[0:8])

	return egp, nil
}

// NewMonitor creates a new monitor instance
func NewMonitor(client *Client) *Monitor {
	return &Monitor{
		client:              client,
		interval:            2 * time.Second,  // Default monitoring interval
		subscriptionTimeout: 30 * time.Second, // Default subscription timeout
		portStates:          make(map[uint16]uint8),
	}
}

// SetInterval sets the monitoring interval
func (m *Monitor) SetInterval(interval time.Duration) {
	m.interval = interval
}

// SetSubscriptionTimeout sets the subscription request timeout
func (m *Monitor) SetSubscriptionTimeout(timeout time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.subscriptionTimeout = timeout
	if m.subscription != nil {
		m.subscription.SetRequestTimeout(timeout)
	}
}

// Start begins monitoring ptp4l status (polling-based, for backward compatibility)
func (m *Monitor) Start(ctx context.Context) error {
	log.Println("Starting PTP4L monitoring...")

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	// Print initial status
	if err := m.printStatus(); err != nil {
		log.Printf("Failed to get initial status: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			log.Println("Monitoring stopped")
			return ctx.Err()
		case <-ticker.C:
			if err := m.printStatus(); err != nil {
				log.Printf("Failed to get status: %v", err)
				// Continue monitoring even if one attempt fails
			}
		}
	}
}

// StartSubscriptionMonitoring starts monitoring using subscriptions for real-time updates
func (m *Monitor) StartSubscriptionMonitoring(ctx context.Context) error {
	log.Println("Starting PTP4L subscription-based monitoring...")

	// Create subscription manager
	m.subscription = NewSubscriptionManager(m.client, m.client.verbose)

	// Apply stored timeout
	m.mu.RLock()
	timeout := m.subscriptionTimeout
	m.mu.RUnlock()
	m.subscription.SetRequestTimeout(timeout)

	// Set up port state change callback
	m.subscription.OnPortStateChange(func(event PortStateChangeEvent) {
		m.handlePortStateChange(event)
	})

	// Subscribe to notifications
	if err := m.subscription.Subscribe(10 * time.Second); err != nil {
		return fmt.Errorf("failed to start subscription: %v", err)
	}

	// Wait for initial port discovery and print status
	time.Sleep(1 * time.Second)
	if err := m.printStatus(); err != nil {
		log.Printf("Failed to get initial status: %v", err)
	}

	// Wait for context cancellation
	<-ctx.Done()

	// Clean up
	m.subscription.Unsubscribe()
	log.Println("Subscription monitoring stopped")
	return ctx.Err()
}

// handlePortStateChange processes port state change events
func (m *Monitor) handlePortStateChange(event PortStateChangeEvent) {
	m.mu.Lock()
	oldState := m.portStates[event.PortIdentity.PortNumber]
	m.portStates[event.PortIdentity.PortNumber] = event.NewState
	m.mu.Unlock()

	fmt.Printf("\n--- PORT STATE CHANGE ---\n")
	fmt.Printf("Time: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Printf("Port: %s\n", event.PortIdentity.String())
	if oldState != 0 {
		fmt.Printf("State: %s -> %s\n", PortStateString(oldState), PortStateString(event.NewState))
	} else {
		fmt.Printf("State: %s\n", PortStateString(event.NewState))
	}

	// Print full status after state change
	fmt.Println("\n--- Updated Status ---")
	if err := m.printStatus(); err != nil {
		log.Printf("Failed to print updated status: %v", err)
	}
}

// printStatus queries and displays current ptp4l status
func (m *Monitor) printStatus() error {
	fmt.Println("\n--- PTP4L Status ---")

	// Get default data set
	dds, err := m.client.GetDefaultDataSet()
	if err != nil {
		return fmt.Errorf("failed to get default data set: %v", err)
	}

	fmt.Printf("Clock Identity: %s\n", dds.ClockIdentity.String())
	fmt.Printf("Domain: %d\n", dds.DomainNumber)
	fmt.Printf("Priority1: %d, Priority2: %d\n", dds.Priority1, dds.Priority2)
	fmt.Printf("Clock Class: %d, Clock Accuracy: %d\n", dds.ClockQuality.ClockClass, dds.ClockQuality.ClockAccuracy)
	fmt.Printf("Two Step: %t, Slave Only: %t\n", dds.TwoStepFlag, dds.SlaveOnly)
	fmt.Printf("Number of Ports: %d\n", dds.NumberPorts)

	// Try port data set (port 1 is usually the main port)
	if dds.NumberPorts > 0 {
		fmt.Println("\n--- Port Information ---")
		pds, err := m.client.GetPortDataSet(1)
		if err != nil {
			fmt.Printf("Failed to get port data set: %v\n", err)
		} else {
			fmt.Printf("Port Identity: %s\n", pds.PortIdentity.String())
			fmt.Printf("Port State: %s\n", PortStateString(pds.PortState))
			fmt.Printf("Log Announce Interval: %d\n", pds.LogAnnounceInterval)
			fmt.Printf("Log Sync Interval: %d\n", pds.LogSyncInterval)
			fmt.Printf("Delay Mechanism: %d\n", pds.DelayMechanism)
		}
	}

	// Try time properties data set
	fmt.Println("\n--- Time Properties ---")
	tpds, err := m.client.GetTimePropertiesDataSet()
	if err != nil {
		fmt.Printf("Failed to get time properties data set: %v\n", err)
	} else {
		fmt.Printf("Current UTC Offset: %d\n", tpds.CurrentUtcOffset)
		fmt.Printf("UTC Offset Valid: %t\n", tpds.CurrentUtcOffsetValid)
		fmt.Printf("Time Traceable: %t\n", tpds.TimeTraceable)
		fmt.Printf("Frequency Traceable: %t\n", tpds.FrequencyTraceable)
		fmt.Printf("PTP Timescale: %t\n", tpds.PtpTimescale)
		fmt.Printf("Time Source: 0x%02x\n", tpds.TimeSource)
	}

	return nil
}

// sendPMCStyleRequest sends a request using PMC's exact message construction
func (c *Client) sendPMCStyleRequest(managementID uint16, targetPort uint16) (*Message, error) {
	return c.sendPMCStyleRequestWithAction(managementID, targetPort, GET, nil)
}

// sendPMCStyleSetRequest sends a SET request using PMC's exact message construction
func (c *Client) sendPMCStyleSetRequest(managementID uint16, targetPort uint16, data []byte) (*Message, error) {
	return c.sendPMCStyleRequestWithAction(managementID, targetPort, SET, data)
}

// sendPMCStyleRequestWithAction sends a request with specified action using PMC's exact message construction
func (c *Client) sendPMCStyleRequestWithAction(managementID uint16, targetPort uint16, action uint8, payload []byte) (*Message, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Create fresh socket for each request (like PMC does)
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_DGRAM, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to create socket: %v", err)
	}
	defer syscall.Close(fd)

	// Create temporary path for binding
	tempPath := fmt.Sprintf("/var/run/pmc-go.%d.%d", os.Getpid(), atomic.AddInt64(&tempPathCounter, 1))

	// Bind to temporary path
	bindAddr := &syscall.SockaddrUnix{Name: tempPath}
	if err := syscall.Bind(fd, bindAddr); err != nil {
		return nil, fmt.Errorf("failed to bind to %s: %v", tempPath, err)
	}
	defer os.Remove(tempPath)

	if c.verbose {
		log.Printf("Created unconnected Unix datagram socket bound to %s", tempPath)
	}

	// Construct message
	msg := c.constructPMCMessage(managementID, targetPort, action, payload)

	if c.verbose {
		log.Printf("Constructed PMC message (%d bytes): %x", len(msg), msg)
	}

	// Send to ptp4l socket
	destAddr := &syscall.SockaddrUnix{Name: c.udsPath}
	if err := syscall.Sendto(fd, msg, 0, destAddr); err != nil {
		return nil, fmt.Errorf("failed to send to %s: %v", c.udsPath, err)
	}

	// Read response
	respBuf := make([]byte, 1500)
	n, _, err := syscall.Recvfrom(fd, respBuf, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %v", err)
	}

	// Parse response
	resp := &Message{}
	if err := resp.Decode(respBuf[:n]); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	return resp, nil
}

// constructPMCMessage constructs a management message exactly like PMC does
func (c *Client) constructPMCMessage(managementID uint16, targetPort uint16, action uint8, payload []byte) []byte {
	// For SET requests, use actual payload length; for GET requests, use expected response length
	var dataLen int
	if action == SET && len(payload) > 0 {
		dataLen = len(payload)
	} else {
		dataLen = c.getTLVDataLength(managementID)
	}

	// TLV length = 2 (for mgmt ID) + dataLen
	tlvLen := 2 + dataLen

	// Total message length = PTP header (34) + management fields (14) + TLV header (6) + data
	// PTP header: 34 bytes, Management msg fields: 14 bytes, TLV header: 6 bytes
	msgLen := 34 + 14 + 6 + dataLen

	msg := make([]byte, msgLen)
	offset := 0

	// PTP Header (34 bytes total)
	msg[0] = 0x0d                                        // messageType (MANAGEMENT) | transportSpecific (0)
	msg[1] = 0x02                                        // versionPTP (2) | minorVersionPTP (0)
	binary.BigEndian.PutUint16(msg[2:4], uint16(msgLen)) // messageLength
	msg[4] = c.domain                                    // domainNumber
	msg[5] = 0x00                                        // reserved1
	// flagField[2] - bytes 6-7
	msg[6] = 0x00
	msg[7] = 0x00
	// correction (8 bytes) - bytes 8-15
	for i := 8; i < 16; i++ {
		msg[i] = 0x00
	}
	// reserved2 (4 bytes) - bytes 16-19
	for i := 16; i < 20; i++ {
		msg[i] = 0x00
	}
	// Source Port Identity (10 bytes) - bytes 20-29
	copy(msg[20:28], c.clockID[:])
	binary.BigEndian.PutUint16(msg[28:30], c.portNum)
	// Sequence ID (2 bytes) - bytes 30-31
	binary.BigEndian.PutUint16(msg[30:32], c.seqID)
	c.seqID++
	// Control and logMessageInterval (2 bytes) - bytes 32-33
	msg[32] = 0x00 // control
	msg[33] = 0x7f // logMessageInterval
	offset = 34

	// Management Message specific fields (14 bytes) - bytes 34-47
	// Target Port Identity (10 bytes) - bytes 34-43
	for i := 0; i < 8; i++ {
		msg[offset+i] = 0xff // target clock identity (all 1s = all ports)
	}
	binary.BigEndian.PutUint16(msg[offset+8:offset+10], targetPort) // target port number
	offset += 10

	// startingBoundaryHops, boundaryHops (2 bytes) - bytes 44-45
	msg[offset] = 0x01   // startingBoundaryHops
	msg[offset+1] = 0x01 // boundaryHops
	offset += 2

	// flags (actionField) and reserved (2 bytes) - bytes 46-47
	msg[offset] = action
	msg[offset+1] = 0x00 // reserved
	offset += 2

	// Management TLV (6 + dataLen bytes) - starts at byte 48
	// TLV type (2 bytes) - bytes 48-49
	binary.BigEndian.PutUint16(msg[offset:offset+2], 0x0001) // TLV_MANAGEMENT
	offset += 2

	// TLV length (2 bytes) - bytes 50-51
	binary.BigEndian.PutUint16(msg[offset:offset+2], uint16(tlvLen))
	offset += 2

	// Management ID (2 bytes) - bytes 52-53
	binary.BigEndian.PutUint16(msg[offset:offset+2], managementID)
	offset += 2

	// Data field (dataLen bytes) - starts at byte 54
	if action == SET && len(payload) > 0 {
		// For SET requests, use the actual payload data
		copy(msg[offset:offset+dataLen], payload)
	} else {
		// For GET requests, fill with zeros (expected response structure)
		for i := 0; i < dataLen; i++ {
			msg[offset+i] = 0x00
		}
	}

	return msg
}

// getTLVDataLength returns the expected data length for a management ID (like pmc_tlv_datalen)
func (c *Client) getTLVDataLength(managementID uint16) int {
	switch managementID {
	case MID_DEFAULT_DATA_SET:
		return 20 // sizeof(struct defaultDS)
	case MID_CURRENT_DATA_SET:
		return 18 // sizeof(struct currentDS) - UInteger16 + TimeInterval + TimeInterval
	case MID_PARENT_DATA_SET:
		return 32 // sizeof(struct parentDS) - PortIdentity(10) + UInteger8 + UInteger8 + UInteger16 + Integer32 + UInteger8 + ClockQuality(4) + UInteger8 + ClockIdentity(8)
	case MID_TIME_PROPERTIES_DATA_SET:
		return 4 // sizeof(struct timePropertiesDS) - Integer16 + UInteger8 + Enumeration8
	case MID_PORT_DATA_SET:
		return 26 // sizeof(struct portDS)
	case MID_GRANDMASTER_SETTINGS_NP:
		return 8 // sizeof(struct grandmaster_settings_np) - ClockQuality(4) + Integer16 + UInteger8 + Enumeration8
	case MID_EXTERNAL_GRANDMASTER_PROPERTIES_NP:
		return 10 // sizeof(struct external_grandmaster_properties_np) - ClockIdentity(8) + UInteger16
	case MID_PRIORITY1, MID_PRIORITY2, MID_DOMAIN, MID_SLAVE_ONLY:
		return 2 // sizeof(struct management_tlv_datum)
	case MID_CLOCK_ACCURACY, MID_LOG_ANNOUNCE_INTERVAL, MID_ANNOUNCE_RECEIPT_TIMEOUT:
		return 2 // sizeof(struct management_tlv_datum)
	case MID_LOG_SYNC_INTERVAL, MID_VERSION_NUMBER:
		return 2 // sizeof(struct management_tlv_datum)
	default:
		return 0 // Unknown management ID
	}
}

// SetGrandmasterSettingsNP sets the grandmaster settings (non-standard)
func (c *Client) SetGrandmasterSettingsNP(gs *GrandmasterSettingsNP) error {
	// Encode the data structure
	data := make([]byte, 8)
	data[0] = gs.ClockQuality.ClockClass
	data[1] = gs.ClockQuality.ClockAccuracy
	binary.BigEndian.PutUint16(data[2:4], gs.ClockQuality.OffsetScaledLogVariance)
	binary.BigEndian.PutUint16(data[4:6], uint16(gs.UtcOffset))
	data[6] = gs.TimeFlags
	data[7] = gs.TimeSource

	// Send SET request
	resp, err := c.sendPMCStyleSetRequest(MID_GRANDMASTER_SETTINGS_NP, 0, data)
	if err != nil {
		return err
	}

	// Check if response indicates success
	if resp.TLV != nil && resp.TLV.Type == TLV_MANAGEMENT_ERROR_STATUS {
		return fmt.Errorf("SET GRANDMASTER_SETTINGS_NP failed with error")
	}

	return nil
}

// SetExternalGrandmasterPropertiesNP sets the external grandmaster properties (non-standard)
func (c *Client) SetExternalGrandmasterPropertiesNP(egp *ExternalGrandmasterPropertiesNP) error {
	// Encode the data structure
	data := make([]byte, 10)
	copy(data[0:8], egp.GmIdentity[:])
	binary.BigEndian.PutUint16(data[8:10], egp.StepsRemoved)

	// Send SET request
	resp, err := c.sendPMCStyleSetRequest(MID_EXTERNAL_GRANDMASTER_PROPERTIES_NP, 0, data)
	if err != nil {
		return err
	}

	// Check if response indicates success
	if resp.TLV != nil && resp.TLV.Type == TLV_MANAGEMENT_ERROR_STATUS {
		return fmt.Errorf("SET EXTERNAL_GRANDMASTER_PROPERTIES_NP failed with error")
	}

	return nil
}

// SendRequest sends a management request and waits for response (PMC-style implementation)
func (c *Client) SendRequest(managementID uint16, targetPort uint16) (*Message, error) {
	return c.sendPMCStyleRequest(managementID, targetPort)
}

// GetPortDataSet retrieves the port data set from ptp4l
func (c *Client) GetPortDataSet(portNum uint16) (*PortDataSet, error) {
	resp, err := c.sendPMCStyleRequest(MID_PORT_DATA_SET, portNum)
	if err != nil {
		return nil, err
	}

	if resp.TLV == nil || len(resp.TLV.Data) < 26 {
		return nil, fmt.Errorf("invalid PORT_DATA_SET response")
	}

	data := resp.TLV.Data
	pds := &PortDataSet{}

	copy(pds.PortIdentity.ClockIdentity[:], data[0:8])
	pds.PortIdentity.PortNumber = binary.BigEndian.Uint16(data[8:10])
	pds.PortState = data[10]
	pds.LogMinDelayReqInterval = int8(data[11])
	pds.PeerMeanPathDelay = int64(binary.BigEndian.Uint64(data[12:20]))
	pds.LogAnnounceInterval = int8(data[20])
	pds.AnnounceReceiptTimeout = data[21]
	pds.LogSyncInterval = int8(data[22])
	pds.DelayMechanism = data[23]
	pds.LogMinPdelayReqInterval = int8(data[24])
	pds.VersionNumber = data[25]

	return pds, nil
}

// GetTimePropertiesDataSet retrieves the time properties data set from ptp4l
func (c *Client) GetTimePropertiesDataSet() (*TimePropertiesDataSet, error) {
	resp, err := c.sendPMCStyleRequest(MID_TIME_PROPERTIES_DATA_SET, 0)
	if err != nil {
		return nil, err
	}

	if resp.TLV == nil || len(resp.TLV.Data) < 4 {
		return nil, fmt.Errorf("invalid TIME_PROPERTIES_DATA_SET response")
	}

	data := resp.TLV.Data
	tpds := &TimePropertiesDataSet{
		CurrentUtcOffset: int16(binary.BigEndian.Uint16(data[0:2])),
		TimeSource:       data[3],
	}

	// Parse flags from data[2]
	flags := data[2]
	tpds.CurrentUtcOffsetValid = (flags & 0x01) != 0
	tpds.Leap59 = (flags & 0x02) != 0
	tpds.Leap61 = (flags & 0x04) != 0
	tpds.TimeTraceable = (flags & 0x08) != 0
	tpds.FrequencyTraceable = (flags & 0x10) != 0
	tpds.PtpTimescale = (flags & 0x20) != 0

	return tpds, nil
}

// ConstructSubscriptionMessage constructs a SUBSCRIBE_EVENTS_NP message for external use
func (c *Client) ConstructSubscriptionMessage(payload []byte) []byte {
	return c.constructPMCMessage(MID_SUBSCRIBE_EVENTS_NP, 0, SET, payload)
}

// GetUDSPath returns the UDS path for external use
func (c *Client) GetUDSPath() string {
	return c.udsPath
}
