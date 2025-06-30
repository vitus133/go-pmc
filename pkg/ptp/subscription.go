package ptp

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Subscription management constants (matching linuxptp)
const (
	UpdatesPerSubscription = 3
	MinUpdateInterval      = 10 * time.Second
	DefaultUpdateInterval  = 60 * time.Second
)

// NotificationCallback is called when notifications are received
type NotificationCallback func(msg *Message) error

// SubscriptionManager handles PTP notification subscriptions
type SubscriptionManager struct {
	client             *Client
	updateInterval     time.Duration
	lastUpdate         time.Time
	isSubscribed       bool
	staySubscribed     bool
	mu                 sync.RWMutex
	ctx                context.Context
	cancel             context.CancelFunc
	callbacks          map[uint16]NotificationCallback // managementID -> callback
	catchAllCallback   NotificationCallback            // Debug callback for all messages
	verbose            bool
	requestTimeout     time.Duration // Timeout for subscription requests
	subscriptionSocket int           // Keep subscription socket open
	tempPath           string        // Path for subscription socket
	subscribedEvents   []uint8       // Store subscribed events for renewal
}

// PortStateChangeEvent represents a port state change notification
type PortStateChangeEvent struct {
	PortIdentity PortIdentity
	OldState     uint8
	NewState     uint8
}

// ParentDataSetChangeEvent represents a parent data set change notification
type ParentDataSetChangeEvent struct {
	ParentPortIdentity                    PortIdentity
	GrandmasterIdentity                   ClockIdentity
	GrandmasterClockQuality               ClockQuality
	GrandmasterPriority1                  uint8
	GrandmasterPriority2                  uint8
	ObservedParentOffsetScaledLogVariance uint16
	ObservedParentClockPhaseChangeRate    int32
}

// TimeStatusChangeEvent represents a time status change notification
type TimeStatusChangeEvent struct {
	MasterOffset               int64         // Offset from master in nanoseconds
	IngressTime                int64         // Ingress timestamp in nanoseconds
	CumulativeScaledRateOffset int32         // Cumulative scaled rate offset
	ScaledLastGmPhaseChange    int32         // Scaled last GM phase change
	GmTimeBaseIndicator        uint16        // GM time base indicator
	LastGmPhaseChange          ClockIdentity // Last GM phase change
	GmPresent                  bool          // GM present flag
	GmIdentity                 ClockIdentity // GM identity
}

// PortStateChangeCallback is called when port state changes
type PortStateChangeCallback func(event PortStateChangeEvent)

// ParentDataSetChangeCallback is called when parent data set changes
type ParentDataSetChangeCallback func(event ParentDataSetChangeEvent)

// TimeStatusChangeCallback is called when time status changes
type TimeStatusChangeCallback func(event TimeStatusChangeEvent)

// NewSubscriptionManager creates a new subscription manager
func NewSubscriptionManager(client *Client, verbose bool) *SubscriptionManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &SubscriptionManager{
		client:         client,
		updateInterval: DefaultUpdateInterval,
		ctx:            ctx,
		cancel:         cancel,
		callbacks:      make(map[uint16]NotificationCallback),
		verbose:        verbose,
		requestTimeout: 30 * time.Second, // Default 30 seconds, much longer than original 5
	}
}

// Subscribe starts a subscription to PTP notifications (default: PORT_STATE only)
func (sm *SubscriptionManager) Subscribe(interval time.Duration) error {
	return sm.SubscribeToPortStateChanges(interval)
}

// Unsubscribe stops the subscription
func (sm *SubscriptionManager) Unsubscribe() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.staySubscribed = false
	sm.isSubscribed = false
	sm.cancel()
	sm.cleanup()

	if sm.verbose {
		log.Println("Subscription stopped")
	}
}

// OnPortStateChange registers a callback for port state changes
func (sm *SubscriptionManager) OnPortStateChange(callback PortStateChangeCallback) {
	sm.callbacks[MID_PORT_DATA_SET] = func(msg *Message) error {
		event, err := sm.parsePortStateChange(msg)
		if err != nil {
			return err
		}
		callback(event)
		return nil
	}
}

// OnNotification registers a callback for any management ID
func (sm *SubscriptionManager) OnNotification(managementID uint16, callback NotificationCallback) {
	if managementID == 0xFFFF {
		// Special case: catch-all callback for debugging
		sm.catchAllCallback = callback
	} else {
		sm.callbacks[managementID] = callback
	}
}

// IsSubscribed returns whether we have an active subscription
func (sm *SubscriptionManager) IsSubscribed() bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if !sm.isSubscribed {
		return false
	}

	// Check if subscription is still valid (like pmc_agent_is_subscribed)
	elapsed := time.Since(sm.lastUpdate)
	maxDuration := UpdatesPerSubscription * sm.updateInterval
	return elapsed <= maxDuration
}

// SetRequestTimeout sets the timeout for subscription requests
func (sm *SubscriptionManager) SetRequestTimeout(timeout time.Duration) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.requestTimeout = timeout
}

// createPersistentSocket creates a persistent socket for subscription
func (sm *SubscriptionManager) createPersistentSocket() error {
	// Create unconnected Unix datagram socket
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_DGRAM, 0)
	if err != nil {
		return fmt.Errorf("failed to create socket: %v", err)
	}

	// Create temporary path for binding
	sm.tempPath = fmt.Sprintf("/var/run/pmc-sub.%d.%d", os.Getpid(), atomic.AddInt64(&tempPathCounter, 1))

	// Bind to temporary path
	bindAddr := &syscall.SockaddrUnix{Name: sm.tempPath}
	if err := syscall.Bind(fd, bindAddr); err != nil {
		syscall.Close(fd)
		return fmt.Errorf("failed to bind to %s: %v", sm.tempPath, err)
	}

	sm.subscriptionSocket = fd

	if sm.verbose {
		log.Printf("Created persistent subscription socket bound to %s (fd=%d)", sm.tempPath, sm.subscriptionSocket)
	}

	return nil
}

// renewalLoop handles periodic subscription renewal
func (sm *SubscriptionManager) renewalLoop() {
	ticker := time.NewTicker(sm.updateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-sm.ctx.Done():
			return
		case <-ticker.C:
			sm.mu.Lock()
			if sm.staySubscribed {
				// Renew with the same events as the original subscription
				if err := sm.sendSubscriptionWithEvents(sm.subscribedEvents...); err != nil {
					if sm.verbose {
						log.Printf("Failed to renew subscription: %v", err)
					}
				} else {
					sm.lastUpdate = time.Now()
					if sm.verbose {
						log.Println("Subscription renewed")
					}
				}
			}
			sm.mu.Unlock()
		}
	}
}

// listen continuously listens for incoming notifications on the persistent socket
func (sm *SubscriptionManager) listen() {
	if sm.verbose {
		log.Printf("Starting notification listener on persistent socket (fd=%d)", sm.subscriptionSocket)
	}

	buffer := make([]byte, 1500)

	for {
		select {
		case <-sm.ctx.Done():
			if sm.verbose {
				log.Printf("Listener stopped by context cancellation")
			}
			return
		default:
			// Set read timeout
			timeout := &syscall.Timeval{Sec: 1, Usec: 0}
			if err := syscall.SetsockoptTimeval(sm.subscriptionSocket, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, timeout); err != nil {
				if sm.verbose {
					log.Printf("Failed to set socket timeout: %v", err)
				}
				return
			}

			n, _, err := syscall.Recvfrom(sm.subscriptionSocket, buffer, 0)
			if err != nil {
				if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
					// Timeout - continue listening
					continue
				}
				if sm.verbose {
					log.Printf("Error reading from subscription socket: %v", err)
				}
				continue
			}

			if sm.verbose {
				log.Printf("🎯 Received notification: %d bytes", n)
				log.Printf("Raw data: %x", buffer[:min(n, 64)])
			}

			// Parse and handle the notification
			sm.handleNotification(buffer[:n])
		}
	}
}

// handleNotification processes incoming notification messages
func (sm *SubscriptionManager) handleNotification(data []byte) {
	if sm.verbose {
		log.Printf("Parsing notification message (%d bytes)", len(data))
	}

	// Parse message
	msg := &Message{}
	if err := msg.Decode(data); err != nil {
		if sm.verbose {
			log.Printf("Failed to decode notification: %v", err)
		}
		return
	}

	if sm.verbose && msg.TLV != nil {
		log.Printf("📨 Notification: MID=0x%04x, Type=0x%04x, Length=%d",
			msg.TLV.ManagementID, msg.TLV.Type, msg.TLV.Length)
	}

	// Handle callbacks
	if msg.TLV != nil {
		sm.mu.RLock()
		callback, exists := sm.callbacks[msg.TLV.ManagementID]
		sm.mu.RUnlock()

		if exists {
			if sm.verbose {
				log.Printf("🔥 Executing callback for MID 0x%04x", msg.TLV.ManagementID)
			}
			if err := callback(msg); err != nil {
				if sm.verbose {
					log.Printf("Callback error: %v", err)
				}
			}
		} else if sm.verbose {
			log.Printf("No callback registered for MID 0x%04x", msg.TLV.ManagementID)
		}
	}
}

// parsePortStateChange extracts port state change information from notification
func (sm *SubscriptionManager) parsePortStateChange(msg *Message) (PortStateChangeEvent, error) {
	if msg.TLV.ManagementID != MID_PORT_DATA_SET {
		return PortStateChangeEvent{}, fmt.Errorf("not a PORT_DATA_SET message")
	}

	if len(msg.TLV.Data) < 20 {
		return PortStateChangeEvent{}, fmt.Errorf("invalid PORT_DATA_SET data length")
	}

	data := msg.TLV.Data
	event := PortStateChangeEvent{}

	// Parse PortIdentity (10 bytes)
	copy(event.PortIdentity.ClockIdentity[:], data[0:8])
	event.PortIdentity.PortNumber = uint16(data[8])<<8 | uint16(data[9])

	// Parse port state (1 byte at offset 10)
	event.NewState = data[10]

	// Note: We don't have the old state from the notification itself.
	// In a real implementation, you'd track port states separately.
	event.OldState = 0 // Would need to be tracked separately

	return event, nil
}

// cleanup closes sockets and removes temporary files
func (sm *SubscriptionManager) cleanup() {
	if sm.subscriptionSocket > 0 {
		syscall.Close(sm.subscriptionSocket)
		sm.subscriptionSocket = 0
	}
	if sm.tempPath != "" {
		os.Remove(sm.tempPath)
		sm.tempPath = ""
	}
}

// Helper function for min
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// SubscribeToEvents subscribes to specific notification events
func (sm *SubscriptionManager) SubscribeToEvents(interval time.Duration, events ...uint8) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Ensure minimum interval
	if interval < MinUpdateInterval {
		interval = MinUpdateInterval
	}
	sm.updateInterval = interval
	sm.staySubscribed = true

	// Create persistent socket for subscription
	if err := sm.createPersistentSocket(); err != nil {
		return fmt.Errorf("failed to create persistent socket: %v", err)
	}

	// Store events for renewal
	sm.subscribedEvents = make([]uint8, len(events))
	copy(sm.subscribedEvents, events)

	// Send initial subscription request with custom events
	if err := sm.sendSubscriptionWithEvents(events...); err != nil {
		sm.cleanup()
		return fmt.Errorf("failed to send subscription: %v", err)
	}

	sm.isSubscribed = true
	sm.lastUpdate = time.Now()

	// Start background listener on the persistent socket
	go sm.listen()
	go sm.renewalLoop()

	if sm.verbose {
		log.Printf("Subscription started with interval %v, events: %v", interval, events)
	}

	return nil
}

// SubscribeToPortStateChanges subscribes only to port state changes
func (sm *SubscriptionManager) SubscribeToPortStateChanges(interval time.Duration) error {
	return sm.SubscribeToEvents(interval, NOTIFY_PORT_STATE)
}

// SubscribeToTimeSync subscribes only to time synchronization events
func (sm *SubscriptionManager) SubscribeToTimeSync(interval time.Duration) error {
	return sm.SubscribeToEvents(interval, NOTIFY_TIME_SYNC)
}

// SubscribeToParentDataSetChanges subscribes only to parent data set changes
func (sm *SubscriptionManager) SubscribeToParentDataSetChanges(interval time.Duration) error {
	return sm.SubscribeToEvents(interval, NOTIFY_PARENT_DATA_SET)
}

// SubscribeToAll subscribes to all available notification types
func (sm *SubscriptionManager) SubscribeToAll(interval time.Duration) error {
	return sm.SubscribeToEvents(interval, NOTIFY_PORT_STATE, NOTIFY_TIME_SYNC, NOTIFY_PARENT_DATA_SET)
}

// sendSubscriptionWithEvents sends a SUBSCRIBE_EVENTS_NP request with custom event mask
func (sm *SubscriptionManager) sendSubscriptionWithEvents(events ...uint8) error {
	// Calculate subscription duration (in seconds)
	duration := uint16(UpdatesPerSubscription * sm.updateInterval / time.Second)

	// Create subscription payload
	payload := make([]byte, 66) // 2 + 64 bytes
	payload[0] = uint8(duration >> 8)
	payload[1] = uint8(duration & 0xFF)

	// Set event bitmask
	var eventMask uint8 = 0
	for _, event := range events {
		eventMask |= event
	}
	payload[2] = eventMask

	if sm.verbose {
		log.Printf("Sending subscription: duration=%d seconds, event_mask=0x%02x", duration, eventMask)
		for _, event := range events {
			switch event {
			case NOTIFY_PORT_STATE:
				log.Printf("  - NOTIFY_PORT_STATE enabled")
			case NOTIFY_TIME_SYNC:
				log.Printf("  - NOTIFY_TIME_SYNC enabled")
			case NOTIFY_PARENT_DATA_SET:
				log.Printf("  - NOTIFY_PARENT_DATA_SET enabled")
			default:
				log.Printf("  - Unknown event type 0x%02x enabled", event)
			}
		}
	}

	// Construct message using client's public method
	msg := sm.client.ConstructSubscriptionMessage(payload)

	// Send via persistent socket
	destAddr := &syscall.SockaddrUnix{Name: sm.client.GetUDSPath()}
	if err := syscall.Sendto(sm.subscriptionSocket, msg, 0, destAddr); err != nil {
		return fmt.Errorf("failed to send subscription request: %v", err)
	}

	// Read immediate response
	respBuf := make([]byte, 1500)
	n, _, err := syscall.Recvfrom(sm.subscriptionSocket, respBuf, 0)
	if err != nil {
		return fmt.Errorf("failed to read subscription response: %v", err)
	}

	if sm.verbose {
		log.Printf("Subscription request sent successfully, response: %d bytes", n)
	}

	return nil
}

// OnTimeStatusChange registers a callback for time status changes (replaces OnTimeSyncChange)
func (sm *SubscriptionManager) OnTimeStatusChange(callback TimeStatusChangeCallback) {
	sm.callbacks[MID_TIME_STATUS_NP] = func(msg *Message) error {
		event, err := sm.parseTimeStatusChange(msg)
		if err != nil {
			return err
		}
		callback(event)
		return nil
	}
}

// OnTimeSyncChange registers a callback for time synchronization events (kept for backward compatibility)
func (sm *SubscriptionManager) OnTimeSyncChange(callback NotificationCallback) {
	sm.callbacks[MID_TIME_STATUS_NP] = callback
}

// OnParentDataSetChange registers a callback for parent data set changes
func (sm *SubscriptionManager) OnParentDataSetChange(callback ParentDataSetChangeCallback) {
	sm.callbacks[MID_PARENT_DATA_SET] = func(msg *Message) error {
		event, err := sm.parseParentDataSetChange(msg)
		if err != nil {
			return err
		}
		callback(event)
		return nil
	}
}

// parseParentDataSetChange extracts parent data set change information from notification
func (sm *SubscriptionManager) parseParentDataSetChange(msg *Message) (ParentDataSetChangeEvent, error) {
	if msg.TLV.ManagementID != MID_PARENT_DATA_SET {
		return ParentDataSetChangeEvent{}, fmt.Errorf("not a PARENT_DATA_SET message")
	}

	if len(msg.TLV.Data) < 32 {
		return ParentDataSetChangeEvent{}, fmt.Errorf("invalid PARENT_DATA_SET data length")
	}

	data := msg.TLV.Data
	event := ParentDataSetChangeEvent{}

	// Parse ParentPortIdentity (10 bytes)
	copy(event.ParentPortIdentity.ClockIdentity[:], data[0:8])
	event.ParentPortIdentity.PortNumber = binary.BigEndian.Uint16(data[8:10])

	// Skip parentStats (1 byte at offset 10) and reserved (1 byte at offset 11)

	// Parse ObservedParentOffsetScaledLogVariance (2 bytes at offset 12)
	event.ObservedParentOffsetScaledLogVariance = binary.BigEndian.Uint16(data[12:14])

	// Parse ObservedParentClockPhaseChangeRate (4 bytes at offset 14)
	event.ObservedParentClockPhaseChangeRate = int32(binary.BigEndian.Uint32(data[14:18]))

	// Parse GrandmasterPriority1 (1 byte at offset 18)
	event.GrandmasterPriority1 = data[18]

	// Parse GrandmasterClockQuality (4 bytes at offset 19)
	event.GrandmasterClockQuality = ClockQuality{
		ClockClass:              data[19],
		ClockAccuracy:           data[20],
		OffsetScaledLogVariance: binary.BigEndian.Uint16(data[21:23]),
	}

	// Parse GrandmasterPriority2 (1 byte at offset 23)
	event.GrandmasterPriority2 = data[23]

	// Parse GrandmasterIdentity (8 bytes at offset 24)
	copy(event.GrandmasterIdentity[:], data[24:32])

	return event, nil
}

// parseTimeStatusChange extracts time status change information from notification
func (sm *SubscriptionManager) parseTimeStatusChange(msg *Message) (TimeStatusChangeEvent, error) {
	if msg.TLV.ManagementID != MID_TIME_STATUS_NP {
		return TimeStatusChangeEvent{}, fmt.Errorf("not a TIME_STATUS_NP message")
	}

	if len(msg.TLV.Data) < 50 {
		return TimeStatusChangeEvent{}, fmt.Errorf("invalid TIME_STATUS_NP data length: got %d, need at least 50", len(msg.TLV.Data))
	}

	data := msg.TLV.Data
	event := TimeStatusChangeEvent{}

	if sm.verbose {
		log.Printf("TIME_STATUS_NP data (%d bytes): %x", len(data), data)
	}

	// Parse master offset (8 bytes at offset 0) - this is the most important field
	event.MasterOffset = int64(binary.BigEndian.Uint64(data[0:8]))

	// Parse ingress time (8 bytes at offset 8)
	event.IngressTime = int64(binary.BigEndian.Uint64(data[8:16]))

	// Parse cumulative scaled rate offset (4 bytes at offset 16)
	event.CumulativeScaledRateOffset = int32(binary.BigEndian.Uint32(data[16:20]))

	// Parse scaled last GM phase change (4 bytes at offset 20)
	event.ScaledLastGmPhaseChange = int32(binary.BigEndian.Uint32(data[20:24]))

	// Parse GM time base indicator (2 bytes at offset 24)
	event.GmTimeBaseIndicator = binary.BigEndian.Uint16(data[24:26])

	// Skip reserved bytes (2 bytes at offset 26)

	// Parse last GM phase change (8 bytes at offset 28)
	if len(data) > 36 {
		copy(event.LastGmPhaseChange[:], data[28:36])
	}

	// Parse GM present flag (1 byte at offset 36)
	if len(data) > 36 {
		event.GmPresent = data[36] != 0
	}

	// Skip reserved bytes (3 bytes at offset 37)

	// Parse GM identity (8 bytes at offset 40)
	if len(data) >= 48 {
		copy(event.GmIdentity[:], data[40:48])
	}

	// Note: There may be additional fields depending on the exact TIME_STATUS_NP implementation

	return event, nil
}
