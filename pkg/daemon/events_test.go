package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReadCloudHypervisorEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ch-events.json")

	events, err := readCloudHypervisorEvents(path)
	assert.NoError(t, err)
	assert.Empty(t, events)

	content := `{
  "timestamp": {"secs": 0, "nanos": 1},
  "source": "vmm",
  "event": "starting",
  "properties": null
}

{"timestamp":{"secs":1,"nanos":0},"source":"vm","event":"migration-started","properties":null}

{"timestamp":{"secs":2,"nanos":0},"source":"vm","event":"migration-fai`
	assert.NoError(t, os.WriteFile(path, []byte(content), 0644))

	events, err = readCloudHypervisorEvents(path)
	assert.NoError(t, err)
	assert.Equal(t, []cloudHypervisorEvent{{Source: "vmm", Event: "starting"}, {Source: "vm", Event: "migration-started"}}, events)
	assert.True(t, hasCloudHypervisorEvent(events, "vm", "migration-started"))
	assert.False(t, hasCloudHypervisorEvent(events[1:], "vmm", "starting"))
}
