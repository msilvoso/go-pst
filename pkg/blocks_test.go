// go-pst is a library for reading Personal Storage Table (.pst) files (written in Go/Golang).
//
// Copyright 2023 Marten Mooij
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pst_test

import (
	"bytes"
	"compress/zlib"
	"io"
	"testing"

	pst "github.com/mooijtech/go-pst/v6/pkg"
)

// TestGetBlockReaderCompressed verifies that zlib compressed blocks (OST files,
// Unicode 4k format) are transparently decompressed.
func TestGetBlockReaderCompressed(t *testing.T) {
	blockData := bytes.Repeat([]byte("go-pst"), 512)

	var compressedBlockData bytes.Buffer

	zlibWriter := zlib.NewWriter(&compressedBlockData)

	if _, err := zlibWriter.Write(blockData); err != nil {
		t.Fatalf("Failed to compress block data: %+v", err)
	}
	if err := zlibWriter.Close(); err != nil {
		t.Fatalf("Failed to close zlib writer: %+v", err)
	}

	file := &pst.File{
		Reader:     pst.NewDefaultReader(bytes.NewReader(compressedBlockData.Bytes())),
		FormatType: pst.FormatTypeUnicode4k,
	}

	btreeNode := pst.BTreeNode{
		FileOffset:   0,
		Size:         uint16(compressedBlockData.Len()),
		InflatedSize: uint16(len(blockData)),
	}

	if !file.IsBlockCompressed(btreeNode) {
		t.Fatal("Expected block to be detected as compressed")
	}

	blockReader, err := file.GetBlockReader(btreeNode)

	if err != nil {
		t.Fatalf("Failed to get block reader: %+v", err)
	}

	if blockReader.Size() != int64(len(blockData)) {
		t.Fatalf("Expected block reader size %d, got %d", len(blockData), blockReader.Size())
	}

	decompressedBlockData, err := io.ReadAll(io.NewSectionReader(blockReader, 0, blockReader.Size()))

	if err != nil {
		t.Fatalf("Failed to read decompressed block data: %+v", err)
	}

	if !bytes.Equal(decompressedBlockData, blockData) {
		t.Fatal("Decompressed block data does not match the original block data")
	}
}

// TestGetBlockReaderUncompressed verifies that blocks with a matching stored
// and inflated size are read as-is.
func TestGetBlockReaderUncompressed(t *testing.T) {
	blockData := bytes.Repeat([]byte("go-pst"), 512)

	for _, formatType := range []pst.FormatType{pst.FormatTypeUnicode, pst.FormatTypeUnicode4k} {
		file := &pst.File{
			Reader:     pst.NewDefaultReader(bytes.NewReader(blockData)),
			FormatType: formatType,
		}

		btreeNode := pst.BTreeNode{
			FileOffset:   0,
			Size:         uint16(len(blockData)),
			InflatedSize: uint16(len(blockData)),
		}

		if file.IsBlockCompressed(btreeNode) {
			t.Fatal("Expected block to be detected as uncompressed")
		}

		blockReader, err := file.GetBlockReader(btreeNode)

		if err != nil {
			t.Fatalf("Failed to get block reader: %+v", err)
		}

		readBlockData, err := io.ReadAll(io.NewSectionReader(blockReader, 0, blockReader.Size()))

		if err != nil {
			t.Fatalf("Failed to read block data: %+v", err)
		}

		if !bytes.Equal(readBlockData, blockData) {
			t.Fatal("Block data does not match the original block data")
		}
	}
}
