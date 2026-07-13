package core

import "github.com/bytedance/sonic"

// unmarshalJSON decodes JSON into v. Sonic is used for the hot parse paths
// (JSONL session logs and Cursor SQLite blobs); encoding/json remains for
// CLI output encoding where compatibility matters more than throughput.
func unmarshalJSON(data []byte, v any) error {
	return sonic.Unmarshal(data, v)
}
