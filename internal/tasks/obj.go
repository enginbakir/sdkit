package tasks

import (
	"bytes"
	"encoding/json"
)

// obj is a JSON object that keeps its key order, so `ids` leaves the file as it was.
type obj struct {
	k []string
	v map[string]json.RawMessage
}

func parseObj(data []byte) (*obj, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	o := &obj{v: map[string]json.RawMessage{}}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, err
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		o.setRaw(t.(string), raw)
	}
	return o, nil
}

func (o *obj) setRaw(k string, raw json.RawMessage) {
	if _, ok := o.v[k]; !ok {
		o.k = append(o.k, k)
	}
	o.v[k] = raw
}

func (o *obj) set(k string, v any) { o.setRaw(k, mustJSON(v)) }

// MarshalJSON writes the keys in their first-seen order.
func (o *obj) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.k {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := marshal(k)
		b.Write(kb)
		b.WriteByte(':')
		b.Write(o.v[k])
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}
