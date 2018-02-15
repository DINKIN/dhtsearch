package dht

import (
	"errors"
	"fmt"
	"math/rand"
	"net"
	"strconv"
)

const transIDBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func newTransactionID() string {
	b := make([]byte, 2)
	for i := range b {
		b[i] = transIDBytes[rand.Int63()%int64(len(transIDBytes))]
	}
	return string(b)
}

// makeQuery returns a query-formed data.
func makeQuery(t, q string, a map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"t": t,
		"y": "q",
		"q": q,
		"a": a,
	}
}

// makeResponse returns a response-formed data.
func makeResponse(t string, r map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"t": t,
		"y": "r",
		"r": r,
	}
}

func getStringKey(data map[string]interface{}, key string) (string, error) {
	val, ok := data[key]
	if !ok {
		return "", fmt.Errorf("krpc: missing key %s", key)
	}
	out, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("krpc: key type mismatch")
	}
	return out, nil
}

func getMapKey(data map[string]interface{}, key string) (map[string]interface{}, error) {
	val, ok := data[key]
	if !ok {
		return nil, fmt.Errorf("krpc: missing key %s", key)
	}
	out, ok := val.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("krpc: key type mismatch")
	}
	return out, nil
}

func getListKey(data map[string]interface{}, key string) ([]interface{}, error) {
	val, ok := data[key]
	if !ok {
		return nil, fmt.Errorf("krpc: missing key %s", key)
	}
	out, ok := val.([]interface{})
	if !ok {
		return nil, fmt.Errorf("krpc: key type mismatch")
	}
	return out, nil
}

// parseKeys parses keys. It just wraps parseKey.
func checkKeys(data map[string]interface{}, pairs [][]string) (err error) {
	for _, args := range pairs {
		key, t := args[0], args[1]
		if err = checkKey(data, key, t); err != nil {
			break
		}
	}
	return err
}

// parseKey parses the key in dict data. `t` is type of the keyed value.
// It's one of "int", "string", "map", "list".
func checkKey(data map[string]interface{}, key string, t string) error {
	val, ok := data[key]
	if !ok {
		return fmt.Errorf("krpc: missing key %s", key)
	}

	switch t {
	case "string":
		_, ok = val.(string)
	case "int":
		_, ok = val.(int)
	case "map":
		_, ok = val.(map[string]interface{})
	case "list":
		_, ok = val.([]interface{})
	default:
		return errors.New("krpc: invalid type")
	}

	if !ok {
		return errors.New("krpc: key type mismatch")
	}

	return nil
}

// Swiped from nictuku
func compactNodeInfoToString(cni string) string {
	if len(cni) == 6 {
		return fmt.Sprintf("%d.%d.%d.%d:%d", cni[0], cni[1], cni[2], cni[3], (uint16(cni[4])<<8)|uint16(cni[5]))
	} else if len(cni) == 18 {
		b := []byte(cni[:16])
		return fmt.Sprintf("[%s]:%d", net.IP.String(b), (uint16(cni[16])<<8)|uint16(cni[17]))
	} else {
		return ""
	}
}

func stringToCompactNodeInfo(addr string) ([]byte, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return []byte{}, err
	}
	pInt, err := strconv.ParseInt(port, 10, 64)
	if err != nil {
		return []byte{}, err
	}
	p := int2bytes(pInt)
	if len(p) < 2 {
		p = append(p, p[0])
		p[0] = 0
	}
	return append([]byte(host), p...), nil
}

func int2bytes(val int64) []byte {
	data, j := make([]byte, 8), -1
	for i := 0; i < 8; i++ {
		shift := uint64((7 - i) * 8)
		data[i] = byte((val & (0xff << shift)) >> shift)

		if j == -1 && data[i] != 0 {
			j = i
		}
	}

	if j != -1 {
		return data[j:]
	}
	return data[:1]
}
