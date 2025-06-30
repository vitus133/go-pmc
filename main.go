package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	ptp "ptp4l-monitor/pkg/ptp"

	"github.com/spf13/cobra"
)

var (
	udsPath        string
	domain         int
	targetPort     int
	verbose        bool
	requestTimeout int // Subscription request timeout in seconds
	// Subscription event flags
	subscribePortState     bool // Subscribe to port state changes
	subscribeTimeSync      bool // Subscribe to time synchronization events
	subscribeParentDataSet bool // Subscribe to parent data set changes
	subscribeAll           bool // Subscribe to all available events
	rootCmd                *cobra.Command
)

func init() {
	rootCmd = &cobra.Command{
		Use:   "ptp4l-monitor",
		Short: "PTP4L Management Client (PMC-compatible)",
		Long: `A command-line PTP management client that communicates with ptp4l
via Unix Domain Socket. Compatible with IEEE 1588 management protocol.

Examples:
  # GET commands - Basic clock information
  ptp4l-monitor get DEFAULT_DATA_SET
  ptp4l-monitor get CURRENT_DATA_SET
  ptp4l-monitor get PARENT_DATA_SET
  ptp4l-monitor get TIME_PROPERTIES_DATA_SET
  
  # GET commands - Port-specific information
  ptp4l-monitor get PORT_DATA_SET --port 1
  ptp4l-monitor get PORT_DATA_SET  # defaults to port 1
  
  # GET commands - Simple values
  ptp4l-monitor get PRIORITY1
  ptp4l-monitor get PRIORITY2
  ptp4l-monitor get DOMAIN
  ptp4l-monitor get SLAVE_ONLY
  ptp4l-monitor get CLOCK_ACCURACY
  ptp4l-monitor get LOG_ANNOUNCE_INTERVAL
  ptp4l-monitor get LOG_SYNC_INTERVAL
  ptp4l-monitor get VERSION_NUMBER
  
  # GET commands - Non-standard extensions
  ptp4l-monitor get GRANDMASTER_SETTINGS_NP
  ptp4l-monitor get EXTERNAL_GRANDMASTER_PROPERTIES_NP
  
  # SET commands
  ptp4l-monitor set GRANDMASTER_SETTINGS_NP 248 254 65535 37 0 0 0 1 0 0 160
  ptp4l-monitor set EXTERNAL_GRANDMASTER_PROPERTIES_NP 50:7c:6f:ff:fe:1f:b2:18 1
  
  # Monitoring
  ptp4l-monitor subscribe                  # Subscription-based real-time monitoring
  
  # Using different socket/domain
  ptp4l-monitor --uds /var/run/ptp4l.0.socket --domain 0 get DEFAULT_DATA_SET
  ptp4l-monitor --verbose get TIME_PROPERTIES_DATA_SET`,
	}

	// Global flags
	rootCmd.PersistentFlags().StringVarP(&udsPath, "uds", "s", "/var/run/ptp4l.1.socket", "Path to ptp4l UDS socket")
	rootCmd.PersistentFlags().IntVarP(&domain, "domain", "d", 24, "PTP domain number")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")
	rootCmd.PersistentFlags().IntVarP(&requestTimeout, "timeout", "t", 30, "Subscription request timeout in seconds")

	// Add subcommands
	rootCmd.AddCommand(getCmd)
	rootCmd.AddCommand(setCmd)
	rootCmd.AddCommand(subscribeCmd)
}

var getCmd = &cobra.Command{
	Use:   "get [MANAGEMENT_ID]",
	Short: "Get PTP management information",
	Long: `Retrieve PTP management information from ptp4l.

Available Management IDs:
  DEFAULT_DATA_SET           - Basic clock information
  CURRENT_DATA_SET           - Current synchronization state  
  PARENT_DATA_SET            - Parent clock information
  TIME_PROPERTIES_DATA_SET   - Time properties and UTC info
  PORT_DATA_SET              - Port configuration and state
  GRANDMASTER_SETTINGS_NP    - Grandmaster settings (non-standard)
  EXTERNAL_GRANDMASTER_PROPERTIES_NP - External grandmaster properties (non-standard)
  PRIORITY1                  - Clock priority 1
  PRIORITY2                  - Clock priority 2
  DOMAIN                     - PTP domain number
  SLAVE_ONLY                 - Slave-only flag
  CLOCK_ACCURACY             - Clock accuracy
  LOG_ANNOUNCE_INTERVAL      - Announce message interval
  LOG_SYNC_INTERVAL          - Sync message interval
  VERSION_NUMBER             - PTP version`,
	Args: cobra.ExactArgs(1),
	Run:  runGet,
}

var setCmd = &cobra.Command{
	Use:   "set [MANAGEMENT_ID] [VALUE]",
	Short: "Set PTP management values",
	Long: `Set PTP management values in ptp4l.

Available SET commands:
  set GRANDMASTER_SETTINGS_NP [clockClass] [clockAccuracy] [offsetScaledLogVariance] [currentUtcOffset] [leap61] [leap59] [currentUtcOffsetValid] [ptpTimescale] [timeTraceable] [frequencyTraceable] [timeSource]
  set EXTERNAL_GRANDMASTER_PROPERTIES_NP [gmIdentity] [stepsRemoved]

Examples:
  ptp4l-monitor set GRANDMASTER_SETTINGS_NP 248 254 65535 37 0 0 0 1 0 0 160
  ptp4l-monitor set EXTERNAL_GRANDMASTER_PROPERTIES_NP 50:7c:6f:ff:fe:1f:b2:18 1`,
	Args: cobra.MinimumNArgs(2),
	Run:  runSet,
}

var subscribeCmd = &cobra.Command{
	Use:   "subscribe",
	Short: "Subscribe to PTP4L status updates",
	Long: `Subscribe to real-time updates of PTP4L status changes.

Event Subscription Options:
  --port-events     Subscribe to port state changes (default: true)
  --time-events     Subscribe to time synchronization events (default: false)
  --parent-events   Subscribe to parent data set change events (default: false)
  --all-events      Subscribe to all available notification types (overrides others)

Examples:
  ptp4l-monitor subscribe                             # Port state changes only (default)
  ptp4l-monitor subscribe --time-events              # Port state + time sync events
  ptp4l-monitor subscribe --parent-events            # Port state + parent data set events
  ptp4l-monitor subscribe --all-events               # All notification types
  ptp4l-monitor subscribe --time-events --parent-events   # Time sync + parent events`,
	Run: runSubscribe,
}

func init() {
	getCmd.Flags().IntVarP(&targetPort, "port", "p", 0, "Target port number (0 = all ports)")

	// Subscription event flags for subscribe command
	subscribeCmd.Flags().BoolVar(&subscribePortState, "port-events", true, "Subscribe to port state change notifications")
	subscribeCmd.Flags().BoolVar(&subscribeTimeSync, "time-events", false, "Subscribe to time synchronization notifications")
	subscribeCmd.Flags().BoolVar(&subscribeParentDataSet, "parent-events", false, "Subscribe to parent data set change notifications")
	subscribeCmd.Flags().BoolVar(&subscribeAll, "all-events", false, "Subscribe to all available notification types")
}

func runGet(cmd *cobra.Command, args []string) {
	managementID := strings.ToUpper(args[0])

	if verbose {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
		log.Printf("Connecting to %s, domain %d", udsPath, domain)
	}

	// Create client
	client, err := ptp.NewClient(udsPath, uint8(domain), verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create PTP client: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	// Execute the specific management request
	switch managementID {
	case "DEFAULT_DATA_SET":
		getDefaultDataSet(client)
	case "PORT_DATA_SET":
		getPortDataSet(client, uint16(targetPort))
	case "TIME_PROPERTIES_DATA_SET":
		getTimePropertiesDataSet(client)
	case "PARENT_DATA_SET":
		getParentDataSet(client)
	case "CURRENT_DATA_SET":
		getCurrentDataSet(client)
	case "GRANDMASTER_SETTINGS_NP":
		getGrandmasterSettingsNP(client)
	case "EXTERNAL_GRANDMASTER_PROPERTIES_NP":
		getExternalGrandmasterPropertiesNP(client)
	case "PRIORITY1":
		getSimpleValue(client, ptp.MID_PRIORITY1, "PRIORITY1")
	case "PRIORITY2":
		getSimpleValue(client, ptp.MID_PRIORITY2, "PRIORITY2")
	case "DOMAIN":
		getSimpleValue(client, ptp.MID_DOMAIN, "DOMAIN")
	case "SLAVE_ONLY":
		getSimpleValue(client, ptp.MID_SLAVE_ONLY, "SLAVE_ONLY")
	case "CLOCK_ACCURACY":
		getSimpleValue(client, ptp.MID_CLOCK_ACCURACY, "CLOCK_ACCURACY")
	case "LOG_ANNOUNCE_INTERVAL":
		getSimpleValue(client, ptp.MID_LOG_ANNOUNCE_INTERVAL, "LOG_ANNOUNCE_INTERVAL")
	case "LOG_SYNC_INTERVAL":
		getSimpleValue(client, ptp.MID_LOG_SYNC_INTERVAL, "LOG_SYNC_INTERVAL")
	case "VERSION_NUMBER":
		getSimpleValue(client, ptp.MID_VERSION_NUMBER, "VERSION_NUMBER")
	default:
		fmt.Fprintf(os.Stderr, "Unknown management ID: %s\n", managementID)
		fmt.Fprintf(os.Stderr, "Use 'ptp4l-monitor get --help' to see available IDs\n")
		os.Exit(1)
	}
}

func getDefaultDataSet(client *ptp.Client) {
	dds, err := client.GetDefaultDataSet()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get DEFAULT_DATA_SET: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("DEFAULT_DATA_SET\n")
	fmt.Printf("  twoStepFlag             %t\n", dds.TwoStepFlag)
	fmt.Printf("  slaveOnly               %t\n", dds.SlaveOnly)
	fmt.Printf("  numberPorts             %d\n", dds.NumberPorts)
	fmt.Printf("  priority1               %d\n", dds.Priority1)
	fmt.Printf("  clockClass              %d\n", dds.ClockQuality.ClockClass)
	fmt.Printf("  clockAccuracy           0x%02x\n", dds.ClockQuality.ClockAccuracy)
	fmt.Printf("  offsetScaledLogVariance 0x%04x\n", dds.ClockQuality.OffsetScaledLogVariance)
	fmt.Printf("  priority2               %d\n", dds.Priority2)
	fmt.Printf("  clockIdentity           %s\n", dds.ClockIdentity.String())
	fmt.Printf("  domainNumber            %d\n", dds.DomainNumber)
}

func getTimePropertiesDataSet(client *ptp.Client) {
	tpds, err := client.GetTimePropertiesDataSet()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get TIME_PROPERTIES_DATA_SET: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("TIME_PROPERTIES_DATA_SET\n")
	fmt.Printf("  currentUtcOffset      %d\n", tpds.CurrentUtcOffset)
	fmt.Printf("  leap61                %t\n", tpds.Leap61)
	fmt.Printf("  leap59                %t\n", tpds.Leap59)
	fmt.Printf("  currentUtcOffsetValid %t\n", tpds.CurrentUtcOffsetValid)
	fmt.Printf("  ptpTimescale          %t\n", tpds.PtpTimescale)
	fmt.Printf("  timeTraceable         %t\n", tpds.TimeTraceable)
	fmt.Printf("  frequencyTraceable    %t\n", tpds.FrequencyTraceable)
	fmt.Printf("  timeSource            0x%02x\n", tpds.TimeSource)
}

func getPortDataSet(client *ptp.Client, port uint16) {
	if port == 0 {
		port = 1 // Default to port 1
	}

	pds, err := client.GetPortDataSet(port)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get PORT_DATA_SET: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("PORT_DATA_SET\n")
	fmt.Printf("  portIdentity            %s\n", pds.PortIdentity.String())
	fmt.Printf("  portState               %s\n", ptp.PortStateString(pds.PortState))
	fmt.Printf("  logMinDelayReqInterval  %d\n", pds.LogMinDelayReqInterval)
	fmt.Printf("  peerMeanPathDelay       %d\n", pds.PeerMeanPathDelay)
	fmt.Printf("  logAnnounceInterval     %d\n", pds.LogAnnounceInterval)
	fmt.Printf("  announceReceiptTimeout  %d\n", pds.AnnounceReceiptTimeout)
	fmt.Printf("  logSyncInterval         %d\n", pds.LogSyncInterval)
	fmt.Printf("  delayMechanism          %d\n", pds.DelayMechanism)
	fmt.Printf("  logMinPdelayReqInterval %d\n", pds.LogMinPdelayReqInterval)
	fmt.Printf("  versionNumber           %d\n", pds.VersionNumber)
}

func getParentDataSet(client *ptp.Client) {
	pds, err := client.GetParentDataSet()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get PARENT_DATA_SET: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("PARENT_DATA_SET\n")
	fmt.Printf("  parentPortIdentity                    %s\n", pds.ParentPortIdentity.String())
	fmt.Printf("  parentStats                           %t\n", pds.ParentStats)
	fmt.Printf("  observedParentOffsetScaledLogVariance %d\n", pds.ObservedParentOffsetScaledLogVariance)
	fmt.Printf("  observedParentClockPhaseChangeRate    %d\n", pds.ObservedParentClockPhaseChangeRate)
	fmt.Printf("  grandmasterIdentity                   %s\n", pds.GrandmasterIdentity.String())
	fmt.Printf("  grandmasterClockClass                 %d\n", pds.GrandmasterClockQuality.ClockClass)
	fmt.Printf("  grandmasterClockAccuracy              0x%02x\n", pds.GrandmasterClockQuality.ClockAccuracy)
	fmt.Printf("  grandmasterOffsetScaledLogVariance    0x%04x\n", pds.GrandmasterClockQuality.OffsetScaledLogVariance)
	fmt.Printf("  grandmasterPriority1                  %d\n", pds.GrandmasterPriority1)
	fmt.Printf("  grandmasterPriority2                  %d\n", pds.GrandmasterPriority2)
}

func getCurrentDataSet(client *ptp.Client) {
	cds, err := client.GetCurrentDataSet()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get CURRENT_DATA_SET: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("CURRENT_DATA_SET\n")
	fmt.Printf("  stepsRemoved     %d\n", cds.StepsRemoved)
	fmt.Printf("  offsetFromMaster %d\n", cds.OffsetFromMaster)
	fmt.Printf("  meanPathDelay    %d\n", cds.MeanPathDelay)
}

func getGrandmasterSettingsNP(client *ptp.Client) {
	gs, err := client.GetGrandmasterSettingsNP()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get GRANDMASTER_SETTINGS_NP: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("GRANDMASTER_SETTINGS_NP\n")
	fmt.Printf("  clockClass              %d\n", gs.ClockQuality.ClockClass)
	fmt.Printf("  clockAccuracy           0x%02x\n", gs.ClockQuality.ClockAccuracy)
	fmt.Printf("  offsetScaledLogVariance 0x%04x\n", gs.ClockQuality.OffsetScaledLogVariance)
	fmt.Printf("  currentUtcOffset        %d\n", gs.UtcOffset)

	// Parse timeFlags byte into individual boolean fields (like PMC does)
	fmt.Printf("  leap61                  %d\n", boolToInt((gs.TimeFlags&0x01) != 0))
	fmt.Printf("  leap59                  %d\n", boolToInt((gs.TimeFlags&0x02) != 0))
	fmt.Printf("  currentUtcOffsetValid   %d\n", boolToInt((gs.TimeFlags&0x04) != 0))
	fmt.Printf("  ptpTimescale            %d\n", boolToInt((gs.TimeFlags&0x08) != 0))
	fmt.Printf("  timeTraceable           %d\n", boolToInt((gs.TimeFlags&0x10) != 0))
	fmt.Printf("  frequencyTraceable      %d\n", boolToInt((gs.TimeFlags&0x20) != 0))

	fmt.Printf("  timeSource              0x%02x\n", gs.TimeSource)
}

// Helper function to convert bool to int for display (like PMC)
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func getExternalGrandmasterPropertiesNP(client *ptp.Client) {
	egp, err := client.GetExternalGrandmasterPropertiesNP()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get EXTERNAL_GRANDMASTER_PROPERTIES_NP: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("EXTERNAL_GRANDMASTER_PROPERTIES_NP\n")
	fmt.Printf("  gmIdentity      %s\n", egp.GmIdentity.String())
	fmt.Printf("  stepsRemoved    %d\n", egp.StepsRemoved)
}

func getSimpleValue(client *ptp.Client, managementID uint16, name string) {
	// Use the low-level SendRequest for simple values
	resp, err := client.SendRequest(managementID, uint16(targetPort))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get %s: %v\n", name, err)
		os.Exit(1)
	}

	if resp.TLV == nil || len(resp.TLV.Data) < 2 {
		fmt.Fprintf(os.Stderr, "Invalid response for %s\n", name)
		os.Exit(1)
	}

	// Most simple values are uint8 or uint16
	fmt.Printf("%s\n", name)
	if len(resp.TLV.Data) >= 2 {
		val := resp.TLV.Data[0]
		fmt.Printf("  %s %d\n", strings.ToLower(name), val)
	}
}

func runSubscribe(cmd *cobra.Command, args []string) {
	// Determine which events to subscribe to based on flags
	var events []uint8
	if subscribeAll {
		events = []uint8{ptp.NOTIFY_PORT_STATE, ptp.NOTIFY_TIME_SYNC, ptp.NOTIFY_PARENT_DATA_SET}
	} else {
		if subscribePortState {
			events = append(events, ptp.NOTIFY_PORT_STATE)
		}
		if subscribeTimeSync {
			events = append(events, ptp.NOTIFY_TIME_SYNC)
		}
		if subscribeParentDataSet {
			events = append(events, ptp.NOTIFY_PARENT_DATA_SET)
		}

		// Default to port state if no events specified
		if len(events) == 0 {
			events = append(events, ptp.NOTIFY_PORT_STATE)
		}
	}

	eventNames := []string{}
	for _, event := range events {
		switch event {
		case ptp.NOTIFY_PORT_STATE:
			eventNames = append(eventNames, "PORT_STATE")
		case ptp.NOTIFY_TIME_SYNC:
			eventNames = append(eventNames, "TIME_SYNC")
		case ptp.NOTIFY_PARENT_DATA_SET:
			eventNames = append(eventNames, "PARENT_DATA_SET")
		}
	}

	fmt.Printf("Starting PTP4L subscription monitoring for events: %v... (Press Ctrl+C to stop)\n", eventNames)

	if verbose {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	}

	// Create client
	client, err := ptp.NewClient(udsPath, uint8(domain), verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create PTP client: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	// Create subscription manager directly for more control
	sm := ptp.NewSubscriptionManager(client, verbose)

	// Configure timeout if specified
	if requestTimeout > 0 {
		sm.SetRequestTimeout(time.Duration(requestTimeout) * time.Second)
	}

	// Set up callbacks
	sm.OnPortStateChange(func(event ptp.PortStateChangeEvent) {
		fmt.Printf("\n🔔 PORT STATE CHANGE\n")
		fmt.Printf("Time: %s\n", time.Now().Format("2006-01-02 15:04:05"))
		fmt.Printf("Port: %s\n", event.PortIdentity.String())
		fmt.Printf("New State: %s\n", ptp.PortStateString(event.NewState))
	})

	sm.OnParentDataSetChange(func(event ptp.ParentDataSetChangeEvent) {
		fmt.Printf("\n👑 PARENT DATA SET CHANGE\n")
		fmt.Printf("Time: %s\n", time.Now().Format("2006-01-02 15:04:05"))
		fmt.Printf("Parent Port: %s\n", event.ParentPortIdentity.String())
		fmt.Printf("Grandmaster: %s\n", event.GrandmasterIdentity.String())
		fmt.Printf("Grandmaster Class: %d\n", event.GrandmasterClockQuality.ClockClass)
		fmt.Printf("Grandmaster Priority1: %d\n", event.GrandmasterPriority1)
		fmt.Printf("Grandmaster Priority2: %d\n", event.GrandmasterPriority2)
	})

	sm.OnTimeStatusChange(func(event ptp.TimeStatusChangeEvent) {
		fmt.Printf("\n🕐 TIME STATUS UPDATE\n")
		fmt.Printf("Time: %s\n", time.Now().Format("2006-01-02 15:04:05"))

		fmt.Printf("Master Offset: %d ns\n", event.MasterOffset)

		fmt.Printf("Ingress Time: %d ns\n", event.IngressTime)
		fmt.Printf("Rate Offset: %d\n", event.CumulativeScaledRateOffset)
		fmt.Printf("GM Present: %t\n", event.GmPresent)
		if event.GmPresent {
			fmt.Printf("GM Identity: %s\n", event.GmIdentity.String())
		}
	})

	// Subscribe to the specified events
	if err := sm.SubscribeToEvents(10*time.Second, events...); err != nil {
		fmt.Fprintf(os.Stderr, "Subscription failed: %v\n", err)
		os.Exit(1)
	}

	// Wait for context cancellation
	ctx := cmd.Context()
	<-ctx.Done()

	// Clean up
	sm.Unsubscribe()
	fmt.Println("\nSubscription monitoring stopped")
}

func runSet(cmd *cobra.Command, args []string) {
	managementID := strings.ToUpper(args[0])

	if verbose {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
		log.Printf("Connecting to %s, domain %d", udsPath, domain)
	}

	// Create client
	client, err := ptp.NewClient(udsPath, uint8(domain), verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create PTP client: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	// Execute the specific SET command
	switch managementID {
	case "GRANDMASTER_SETTINGS_NP":
		setGrandmasterSettingsNP(client, args[1:])
	case "EXTERNAL_GRANDMASTER_PROPERTIES_NP":
		setExternalGrandmasterPropertiesNP(client, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "SET operation not supported for: %s\n", managementID)
		fmt.Fprintf(os.Stderr, "Use 'ptp4l-monitor set --help' to see available SET commands\n")
		os.Exit(1)
	}
}

func setGrandmasterSettingsNP(client *ptp.Client, args []string) {
	if len(args) < 11 {
		fmt.Fprintf(os.Stderr, "GRANDMASTER_SETTINGS_NP requires 11 parameters: clockClass clockAccuracy offsetScaledLogVariance currentUtcOffset leap61 leap59 currentUtcOffsetValid ptpTimescale timeTraceable frequencyTraceable timeSource\n")
		os.Exit(1)
	}

	// Parse parameters
	clockClass, err := strconv.ParseUint(args[0], 10, 8)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid clockClass: %v\n", err)
		os.Exit(1)
	}

	clockAccuracy, err := strconv.ParseUint(args[1], 10, 8)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid clockAccuracy: %v\n", err)
		os.Exit(1)
	}

	offsetScaledLogVariance, err := strconv.ParseUint(args[2], 10, 16)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid offsetScaledLogVariance: %v\n", err)
		os.Exit(1)
	}

	utcOffset, err := strconv.ParseInt(args[3], 10, 16)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid utcOffset: %v\n", err)
		os.Exit(1)
	}

	leap61, err := strconv.ParseUint(args[4], 10, 1)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid leap61: %v\n", err)
		os.Exit(1)
	}

	leap59, err := strconv.ParseUint(args[5], 10, 1)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid leap59: %v\n", err)
		os.Exit(1)
	}

	currentUtcOffsetValid, err := strconv.ParseUint(args[6], 10, 1)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid currentUtcOffsetValid: %v\n", err)
		os.Exit(1)
	}

	ptpTimescale, err := strconv.ParseUint(args[7], 10, 1)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid ptpTimescale: %v\n", err)
		os.Exit(1)
	}

	timeTraceable, err := strconv.ParseUint(args[8], 10, 1)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid timeTraceable: %v\n", err)
		os.Exit(1)
	}

	frequencyTraceable, err := strconv.ParseUint(args[9], 10, 1)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid frequencyTraceable: %v\n", err)
		os.Exit(1)
	}

	timeSource, err := strconv.ParseUint(args[10], 10, 8)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid timeSource: %v\n", err)
		os.Exit(1)
	}

	// Construct timeFlags byte from individual boolean values
	var timeFlags uint8
	if leap61 != 0 {
		timeFlags |= 0x01
	}
	if leap59 != 0 {
		timeFlags |= 0x02
	}
	if currentUtcOffsetValid != 0 {
		timeFlags |= 0x04
	}
	if ptpTimescale != 0 {
		timeFlags |= 0x08
	}
	if timeTraceable != 0 {
		timeFlags |= 0x10
	}
	if frequencyTraceable != 0 {
		timeFlags |= 0x20
	}

	// Create GrandmasterSettingsNP struct
	gs := &ptp.GrandmasterSettingsNP{
		ClockQuality: ptp.ClockQuality{
			ClockClass:              uint8(clockClass),
			ClockAccuracy:           uint8(clockAccuracy),
			OffsetScaledLogVariance: uint16(offsetScaledLogVariance),
		},
		UtcOffset:  int16(utcOffset),
		TimeFlags:  timeFlags,
		TimeSource: uint8(timeSource),
	}

	// Send SET request
	if err := client.SetGrandmasterSettingsNP(gs); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to set GRANDMASTER_SETTINGS_NP: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("GRANDMASTER_SETTINGS_NP set successfully")
}

func setExternalGrandmasterPropertiesNP(client *ptp.Client, args []string) {
	if len(args) < 2 {
		fmt.Fprintf(os.Stderr, "EXTERNAL_GRANDMASTER_PROPERTIES_NP requires 2 parameters: gmIdentity stepsRemoved\n")
		os.Exit(1)
	}

	// Parse clock identity
	gmIdentityStr := args[0]
	var gmIdentity ptp.ClockIdentity
	if err := parseClockIdentity(gmIdentityStr, &gmIdentity); err != nil {
		fmt.Fprintf(os.Stderr, "Invalid gmIdentity format: %v\n", err)
		os.Exit(1)
	}

	stepsRemoved, err := strconv.ParseUint(args[1], 10, 16)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid stepsRemoved: %v\n", err)
		os.Exit(1)
	}

	// Create ExternalGrandmasterPropertiesNP struct
	egp := &ptp.ExternalGrandmasterPropertiesNP{
		GmIdentity:   gmIdentity,
		StepsRemoved: uint16(stepsRemoved),
	}

	// Send SET request
	if err := client.SetExternalGrandmasterPropertiesNP(egp); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to set EXTERNAL_GRANDMASTER_PROPERTIES_NP: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("EXTERNAL_GRANDMASTER_PROPERTIES_NP set successfully")
}

func parseClockIdentity(s string, ci *ptp.ClockIdentity) error {
	parts := strings.Split(s, ":")
	if len(parts) != 8 {
		return fmt.Errorf("clock identity must have 8 hex octets separated by colons")
	}

	for i, part := range parts {
		val, err := strconv.ParseUint(part, 16, 8)
		if err != nil {
			return fmt.Errorf("invalid hex octet %d: %v", i, err)
		}
		ci[i] = uint8(val)
	}

	return nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
