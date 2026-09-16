package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// JSONSchemaVersion is the "schemaVersion" of every orb --json output
// (ADR-0015, ADR-0054). Scripts check it before reading the rest. Adding a
// field keeps the version; removing, renaming or retyping one is a breaking
// change that needs a new major version of orb and a new schema version.
const JSONSchemaVersion = 1

// writeJSON writes v, which must encode as a JSON object, as indented JSON
// with "schemaVersion" as its first field.
func writeJSON(w io.Writer, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	body = bytes.TrimSpace(body)
	if len(body) < 2 || body[0] != '{' {
		return fmt.Errorf("orb: --json output must be an object, got %s", body)
	}
	versioned := fmt.Appendf(nil, `{"schemaVersion":%d`, JSONSchemaVersion)
	if rest := body[1:]; !bytes.Equal(rest, []byte("}")) {
		versioned = append(versioned, ',')
		versioned = append(versioned, rest...)
	} else {
		versioned = append(versioned, '}')
	}
	var out bytes.Buffer
	if err := json.Indent(&out, versioned, "", "  "); err != nil {
		return err
	}
	out.WriteByte('\n')
	_, err = w.Write(out.Bytes())
	return err
}
