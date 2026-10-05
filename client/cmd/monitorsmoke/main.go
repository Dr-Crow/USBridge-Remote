// monitorsmoke switches one of an agent's monitors off and back on through the client's own
// API code (the dashboard's "On" switch), checking the device list after each step.
//
//	go run ./cmd/monitorsmoke -host 127.0.0.1 -port 8080 -secret <agent master key> -monitor ARZOPA
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"usbridge-client/internal/api"
	"usbridge-client/internal/models"
)

func main() {
	host := flag.String("host", "127.0.0.1", "agent host")
	port := flag.Int("port", 8080, "agent HTTP port")
	secret := flag.String("secret", "", "agent master key")
	name := flag.String("monitor", "", "part of the monitor's name to switch off and on")
	flag.Parse()
	c := api.NewUSBClient(*host, *port, 10)
	c.SetAPISecretV2([]byte(*secret))
	find := func() *models.SystemDevice {
		devs, err := c.GetVideoDevices()
		if err != nil {
			fmt.Println("list:", err)
			os.Exit(1)
		}
		var hit *models.SystemDevice
		for i := range devs {
			d := devs[i]
			en := "-"
			if d.Enabled != nil {
				en = fmt.Sprint(*d.Enabled)
			}
			fmt.Printf("  %-40s connected=%-5v enabled=%-5s id=%.40s\n", d.Name, d.Connected, en, d.MonitorID)
			if *name != "" && strings.Contains(strings.ToLower(d.Name), strings.ToLower(*name)) && d.MonitorID != "" {
				hit = &devs[i]
			}
		}
		return hit
	}
	fmt.Println("devices:")
	m := find()
	if *name == "" {
		return
	}
	if m == nil {
		fmt.Println("no such monitor")
		os.Exit(1)
	}
	id := m.MonitorID
	for _, on := range []bool{false, true} {
		if err := c.SetMonitorEnabled(id, on); err != nil {
			fmt.Printf("SetMonitorEnabled(%v): %v\n", on, err)
			os.Exit(1)
		}
		time.Sleep(3 * time.Second)
		fmt.Printf("after enabled=%v:\n", on)
		find()
	}
}
