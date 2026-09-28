package balloon

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ReadNodeStats reads node memory stats from procfs. PSI is treated as 0 if the kernel does not support it.
func ReadNodeStats(procPath string) (NodeStats, error) {
	var stats NodeStats
	fields, err := readKBFields(procPath+"/meminfo", "MemTotal:", "MemAvailable:")
	if err != nil {
		return stats, err
	}
	stats.MemTotal, stats.MemAvailable = fields["MemTotal:"], fields["MemAvailable:"]

	psi, err := os.ReadFile(procPath + "/pressure/memory")
	if err != nil {
		if os.IsNotExist(err) {
			return stats, nil
		}
		return stats, err
	}
	stats.PSISomeAvg10, err = parsePSISomeAvg10(string(psi))
	return stats, err
}

// ReadRSS reads the resident memory of the process.
func ReadRSS(procPath string, pid int) (int64, error) {
	fields, err := readKBFields(fmt.Sprintf("%s/%d/status", procPath, pid), "VmRSS:")
	if err != nil {
		return 0, err
	}
	return fields["VmRSS:"], nil
}

func readKBFields(path string, keys ...string) (map[string]int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	fields := map[string]int64{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		if len(parts) < 2 {
			continue
		}
		for _, key := range keys {
			if parts[0] == key {
				value, err := strconv.ParseInt(parts[1], 10, 64)
				if err != nil {
					return nil, fmt.Errorf("parse %s in %s: %s", key, path, err)
				}
				fields[key] = value * 1024
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	for _, key := range keys {
		if _, ok := fields[key]; !ok {
			return nil, fmt.Errorf("%s not found in %s", key, path)
		}
	}
	return fields, nil
}

func parsePSISomeAvg10(psi string) (float64, error) {
	for _, line := range strings.Split(psi, "\n") {
		parts := strings.Fields(line)
		if len(parts) < 2 || parts[0] != "some" {
			continue
		}
		for _, part := range parts[1:] {
			if value, ok := strings.CutPrefix(part, "avg10="); ok {
				return strconv.ParseFloat(value, 64)
			}
		}
	}
	return 0, fmt.Errorf("some avg10 not found in memory PSI")
}
