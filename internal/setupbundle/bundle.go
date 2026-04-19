package setupbundle

import (
	"encoding/binary"
	"fmt"
	"os"
)

var trailerMagic = [16]byte{'W', 'A', 'R', 'D', 'E', 'N', '_', 'S', 'E', 'T', 'U', 'P', '_', 'V', '1', 0}

const trailerSize = 8 + len(trailerMagic)

func Build(bootstrap, payload []byte) []byte {
	out := make([]byte, 0, len(bootstrap)+len(payload)+trailerSize)
	out = append(out, bootstrap...)
	out = append(out, payload...)

	var length [8]byte
	binary.LittleEndian.PutUint64(length[:], uint64(len(payload)))
	out = append(out, length[:]...)
	out = append(out, trailerMagic[:]...)
	return out
}

func Extract(executable []byte) ([]byte, error) {
	if len(executable) < trailerSize {
		return nil, fmt.Errorf("setup bundle trailer missing")
	}

	trailerOffset := len(executable) - trailerSize
	if string(executable[trailerOffset+8:]) != string(trailerMagic[:]) {
		return nil, fmt.Errorf("setup bundle magic mismatch")
	}

	payloadLength := binary.LittleEndian.Uint64(executable[trailerOffset : trailerOffset+8])
	if payloadLength == 0 {
		return nil, fmt.Errorf("setup bundle payload is empty")
	}
	if payloadLength > uint64(trailerOffset) {
		return nil, fmt.Errorf("setup bundle payload length %d exceeds executable size", payloadLength)
	}

	payloadOffset := trailerOffset - int(payloadLength)
	payload := make([]byte, payloadLength)
	copy(payload, executable[payloadOffset:trailerOffset])
	return payload, nil
}

func ExtractFromFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read setup bundle %s: %w", path, err)
	}
	return Extract(data)
}
