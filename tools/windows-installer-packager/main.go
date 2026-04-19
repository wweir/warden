package main

import (
	"archive/zip"
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wweir/warden/internal/setupbundle"
)

func main() {
	var bootstrapPath string
	var runtimePath string
	var outputPath string

	flag.StringVar(&bootstrapPath, "bootstrap", "", "path to bootstrap setup executable")
	flag.StringVar(&runtimePath, "runtime", "", "path to runtime warden.exe")
	flag.StringVar(&outputPath, "output", "", "output setup executable path")
	flag.Parse()

	if bootstrapPath == "" || runtimePath == "" || outputPath == "" {
		flag.Usage()
		os.Exit(2)
	}

	if err := run(bootstrapPath, runtimePath, outputPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(bootstrapPath, runtimePath, outputPath string) error {
	bootstrap, err := os.ReadFile(bootstrapPath)
	if err != nil {
		return fmt.Errorf("read bootstrap %s: %w", bootstrapPath, err)
	}
	runtime, err := os.ReadFile(runtimePath)
	if err != nil {
		return fmt.Errorf("read runtime %s: %w", runtimePath, err)
	}

	payload, err := buildPayload(filepath.Base(runtimePath), runtime)
	if err != nil {
		return err
	}

	bundle := setupbundle.Build(bootstrap, payload)
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create output dir for %s: %w", outputPath, err)
	}
	if err := os.WriteFile(outputPath, bundle, 0o755); err != nil {
		return fmt.Errorf("write setup executable %s: %w", outputPath, err)
	}
	return nil
}

func buildPayload(runtimeName string, runtime []byte) ([]byte, error) {
	var buffer bytes.Buffer
	zipWriter := zip.NewWriter(&buffer)

	header := &zip.FileHeader{
		Name:   runtimeName,
		Method: zip.Deflate,
	}
	header.SetMode(0o755)
	fileWriter, err := zipWriter.CreateHeader(header)
	if err != nil {
		return nil, fmt.Errorf("create payload entry %s: %w", runtimeName, err)
	}
	if _, err := fileWriter.Write(runtime); err != nil {
		return nil, fmt.Errorf("write payload entry %s: %w", runtimeName, err)
	}
	if err := zipWriter.Close(); err != nil {
		return nil, fmt.Errorf("close payload zip: %w", err)
	}
	return buffer.Bytes(), nil
}
