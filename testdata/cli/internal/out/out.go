// Package out holds the JSON output.
package out

// Result is a JSON record.
type Result struct {
	Value  int    `json:"value"`
	Status string `json:"status,omitempty"`
	Secret string `json:"-"`
	Extra  string `json:"extra"`
}
