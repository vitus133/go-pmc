# ptp4l management client in golang #
This is an experimental project written with the help of Cursor AI editor. It implements a simple and limited PTP management client in the Go language.
The purpose of the project is to provide notifications to event-driven applications in Go. This is just a playground and proof of concept.

## Build ##
```bash
git clone https://github.com/vitus133/go-pmc.git ptp4l-monitor
cd ptp4l-monitor
go build -a
```

## Run ##
To get a list of supported commands, just run the program without any command:
```bash
sh-5.1# ./ptp4l-monitor                        
A command-line PTP management client that communicates with ptp4l
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
  ptp4l-monitor --verbose get TIME_PROPERTIES_DATA_SET

Usage:
  ptp4l-monitor [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  get         Get PTP management information
  help        Help about any command
  set         Set PTP management values
  subscribe   Subscribe to PTP4L status updates

Flags:
  -d, --domain int    PTP domain number (default 24)
  -h, --help          help for ptp4l-monitor
  -t, --timeout int   Subscription request timeout in seconds (default 30)
  -s, --uds string    Path to ptp4l UDS socket (default "/var/run/ptp4l.1.socket")
  -v, --verbose       Verbose output

Use "ptp4l-monitor [command] --help" for more information about a command.
```
## Examples ##
Simple get and set commands are shown above. Here is a couple of notification examples:

### Getting ptp4l master offset updates
The ptp4l in this example sends offset notifications 16 times/sec, since it is configured with Telco profile:
```bash
sh-5.1# ./ptp4l-monitor subscribe --all-events
Starting PTP4L subscription monitoring for events: [PORT_STATE TIME_SYNC PARENT_DATA_SET]... (Press Ctrl+C to stop)
2025/06/30 16:52:32 Created unconnected Unix datagram socket bound to /var/run/pmc-go.766796.1

🕐 TIME STATUS UPDATE
Time: 2025-06-30 16:52:32
Master Offset: -1 ns
Ingress Time: 1751302389440125034 ns
Rate Offset: 0
GM Present: false

🕐 TIME STATUS UPDATE
Time: 2025-06-30 16:52:32
Master Offset: 0 ns
Ingress Time: 1751302389502625079 ns
Rate Offset: 0
GM Present: false

🕐 TIME STATUS UPDATE
Time: 2025-06-30 16:52:32
Master Offset: 1 ns
Ingress Time: 1751302389565124961 ns
Rate Offset: 0
GM Present: false

🕐 TIME STATUS UPDATE
Time: 2025-06-30 16:52:32
Master Offset: -1 ns
Ingress Time: 1751302389627625004 ns
Rate Offset: 0
GM Present: false

🕐 TIME STATUS UPDATE
Time: 2025-06-30 16:52:32
Master Offset: -1 ns
Ingress Time: 1751302389690125049 ns
Rate Offset: 0
GM Present: false

🕐 TIME STATUS UPDATE
Time: 2025-06-30 16:52:32
Master Offset: -1 ns
Ingress Time: 1751302389752624930 ns
Rate Offset: 0
GM Present: false

🕐 TIME STATUS UPDATE
Time: 2025-06-30 16:52:32
Master Offset: -2 ns
Ingress Time: 1751302389815125138 ns
Rate Offset: 0
GM Present: false

🕐 TIME STATUS UPDATE
Time: 2025-06-30 16:52:32
Master Offset: -2 ns
Ingress Time: 1751302389877625019 ns
Rate Offset: 0
GM Present: false

🕐 TIME STATUS UPDATE
Time: 2025-06-30 16:52:32
Master Offset: 0 ns
Ingress Time: 1751302389940125064 ns
Rate Offset: 0
GM Present: false

```
### Getting ptp4l parent dataset and port state updates ###
In this example the master port has been set to `down` and back to `up`. We can see the notifications of the port state change and parent dataset change:
```bash
sh-5.1# ./ptp4l-monitor subscribe --parent-events
Starting PTP4L subscription monitoring for events: [PORT_STATE PARENT_DATA_SET]... (Press Ctrl+C to stop)
2025/06/30 16:53:15 Created unconnected Unix datagram socket bound to /var/run/pmc-go.768282.1

🔔 PORT STATE CHANGE
Time: 2025-06-30 16:53:20
Port: 50:7c:6f:ff:fe:1f:b2:18/1
New State: FAULTY

👑 PARENT DATA SET CHANGE
Time: 2025-06-30 16:53:20
Parent Port: 50:7c:6f:ff:fe:1f:b2:18/0
Grandmaster: 50:7c:6f:ff:fe:1f:b2:18
Grandmaster Class: 248
Grandmaster Priority1: 128
Grandmaster Priority2: 128

🔔 PORT STATE CHANGE
Time: 2025-06-30 16:53:21
Port: 50:7c:6f:ff:fe:1f:b2:18/1
New State: LISTENING

👑 PARENT DATA SET CHANGE
Time: 2025-06-30 16:53:22
Parent Port: 00:00:00:00:00:00:00:01/1
Grandmaster: 00:00:00:00:00:00:00:01
Grandmaster Class: 6
Grandmaster Priority1: 128
Grandmaster Priority2: 128

🔔 PORT STATE CHANGE
Time: 2025-06-30 16:53:22
Port: 50:7c:6f:ff:fe:1f:b2:18/1
New State: UNCALIBRATED

🔔 PORT STATE CHANGE
Time: 2025-06-30 16:53:38
Port: 50:7c:6f:ff:fe:1f:b2:18/1
New State: SLAVE
^C
```
User can also subscribe to port events alone:
```bash
sh-5.1# ./ptp4l-monitor subscribe --port-events  
Starting PTP4L subscription monitoring for events: [PORT_STATE]... (Press Ctrl+C to stop)
2025/06/30 16:54:05 Created unconnected Unix datagram socket bound to /var/run/pmc-go.770168.1

🔔 PORT STATE CHANGE
Time: 2025-06-30 16:54:09
Port: 50:7c:6f:ff:fe:1f:b2:18/1
New State: FAULTY

🔔 PORT STATE CHANGE
Time: 2025-06-30 16:54:11
Port: 50:7c:6f:ff:fe:1f:b2:18/1
New State: LISTENING

🔔 PORT STATE CHANGE
Time: 2025-06-30 16:54:11
Port: 50:7c:6f:ff:fe:1f:b2:18/1
New State: UNCALIBRATED

🔔 PORT STATE CHANGE
Time: 2025-06-30 16:54:27
Port: 50:7c:6f:ff:fe:1f:b2:18/1
New State: SLAVE
^C

```
