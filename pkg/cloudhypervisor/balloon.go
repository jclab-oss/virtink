package cloudhypervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// VmResizeBalloon resizes the balloon device. Unlike VmResize, it always sends desired_balloon,
// since the generated VmResize omits a zero value and thus can not fully deflate the balloon.
func (c *Client) VmResizeBalloon(ctx context.Context, size int64) error {
	reqBody, err := json.Marshal(map[string]int64{"desired_balloon": size})
	if err != nil {
		return fmt.Errorf("encode request: %s", err)
	}

	req, err := http.NewRequestWithContext(ctx, "PUT", "http://localhost/api/v1/vm.resize", bytes.NewBuffer(reqBody))
	if err != nil {
		return fmt.Errorf("build request: %s", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %s", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("request failed: %d %s: %s", resp.StatusCode, http.StatusText(resp.StatusCode), string(body))
	}

	return nil
}
