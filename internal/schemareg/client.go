package schemareg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client is a minimal Confluent-compatible schema registry client.
type Client struct {
	baseURL string
	http    *http.Client
}

func New(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// EnsureProtobuf registers the Protobuf schema under subject if it does not
// already exist, then returns the schema ID. Retries until the registry is
// reachable (up to ~30 s).
func (c *Client) EnsureProtobuf(subject, schema string) (int32, error) {
	payload, _ := json.Marshal(map[string]string{
		"schemaType": "PROTOBUF",
		"schema":     schema,
	})
	for i := 0; i < 30; i++ {
		resp, err := c.http.Post(
			c.baseURL+"/subjects/"+subject+"/versions",
			"application/vnd.schemaregistry.v1+json",
			bytes.NewReader(payload),
		)
		if err != nil {
			time.Sleep(time.Second)
			continue
		}
		var result struct {
			ID int32 `json:"id"`
		}
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()
		if result.ID > 0 {
			return result.ID, nil
		}
		time.Sleep(time.Second)
	}
	return 0, fmt.Errorf("schema registry not reachable after 30s")
}
