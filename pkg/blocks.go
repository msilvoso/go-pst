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

package pst

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"

	"github.com/rotisserie/eris"
)

// GetBlockSize returns the size of a block.
// References "Blocks".
func (file *File) GetBlockSize() (int, error) {
	switch file.FormatType {
	case FormatTypeUnicode:
		return 8192, nil
	case FormatTypeUnicode4k:
		return 65536, nil
	case FormatTypeANSI:
		return 8192, nil
	default:
		return 0, ErrFormatTypeUnsupported
	}
}

// GetBlockTrailerSize returns the size of a block trailer.
// The Unicode 4k (OST) block trailer has 8 extra bytes containing the inflated (uncompressed) size.
// References "Blocks".
func (file *File) GetBlockTrailerSize() (int, error) {
	switch file.FormatType {
	case FormatTypeUnicode:
		return 16, nil
	case FormatTypeUnicode4k:
		return 24, nil
	case FormatTypeANSI:
		return 12, nil
	default:
		return 0, ErrFormatTypeUnsupported
	}
}

// IsBlockCompressed returns true if the block data is zlib (DEFLATE) compressed.
// Only the Unicode 4k format (OST files, Outlook 2013 and later) supports compression.
// A block is compressed when its stored size differs from its inflated (uncompressed) size.
func (file *File) IsBlockCompressed(btreeNode BTreeNode) bool {
	return file.FormatType == FormatTypeUnicode4k && btreeNode.InflatedSize != 0 && btreeNode.InflatedSize != btreeNode.Size
}

// GetBlockReader returns a reader for the data of the block b-tree node,
// transparently decompressing zlib compressed blocks (OST files, Outlook 2013 and later).
// Decompression happens before decryption (see HeapOnNodeReader).
func (file *File) GetBlockReader(btreeNode BTreeNode) (*io.SectionReader, error) {
	if !file.IsBlockCompressed(btreeNode) {
		return io.NewSectionReader(file.Reader, btreeNode.FileOffset, int64(btreeNode.Size)), nil
	}

	compressedData := make([]byte, btreeNode.Size)

	if _, err := file.Reader.ReadAt(compressedData, btreeNode.FileOffset); err != nil {
		return nil, eris.Wrap(err, "failed to read compressed block data")
	}

	zlibReader, err := zlib.NewReader(bytes.NewReader(compressedData))

	if err != nil {
		return nil, eris.Wrap(err, "failed to create zlib reader")
	}
	defer zlibReader.Close()

	blockData := make([]byte, btreeNode.InflatedSize)

	if _, err := io.ReadFull(zlibReader, blockData); err != nil {
		return nil, eris.Wrap(err, "failed to decompress block data")
	}

	return io.NewSectionReader(bytes.NewReader(blockData), 0, int64(btreeNode.InflatedSize)), nil
}

// BlockType represents a XBlock or XXBlock.
type BlockType uint8

// Constants defining the block types.
const (
	BlockTypeXBlock  BlockType = 1
	BlockTypeXXBlock BlockType = 2
)

// GetBlocks returns all blocks (XBlock/XXBlock) from a Heap-on-Node along with the total blocks size.
// Internal identifiers have blocks.
//
// References:
// - https://github.com/msilvoso/go-pst/tree/master/docs#xblock
// - https://github.com/msilvoso/go-pst/tree/master/docs#xxblock
func (file *File) GetBlocks(btreeNode BTreeNode) ([]BTreeNode, error) {
	blockReader, err := file.GetBlockReader(btreeNode)

	if err != nil {
		return nil, eris.Wrap(err, "failed to get block reader")
	}

	data := make([]byte, 4)

	if _, err := blockReader.ReadAt(data, 0); err != nil {
		return nil, eris.Wrap(err, "failed to read block data")
	}

	blockSignature := data[0]                                // Must indicate 1.
	blockLevel := data[1]                                    // 1 indicates XBlock, 2 indicates XXBlock.
	entryCount := int(binary.LittleEndian.Uint16(data[2:4])) // The number of block b-tree identifiers in this XBlock or XXBlock.

	if blockSignature != 1 {
		return nil, ErrBlockSignatureInvalid
	}

	identifierSize := int(GetIdentifierSize(file.FormatType))

	var blocks []BTreeNode

	switch BlockType(blockLevel) {
	case BlockTypeXBlock:
		// XBlock
		blockIdentifiers := make([]byte, entryCount*identifierSize)

		if _, err := blockReader.ReadAt(blockIdentifiers, 8); err != nil {
			return nil, eris.Wrap(err, "failed to read block identifiers")
		}

		for i := 0; i < entryCount; i++ {
			blockIdentifier := GetIdentifierFromBytes(blockIdentifiers[i*identifierSize:(i*identifierSize)+identifierSize], file.FormatType)
			blockBTreeNode, err := file.GetBlockBTreeNode(blockIdentifier) // TODO - Async then wait for the block b-tree node lookups.

			if err != nil {
				return nil, eris.Wrap(err, "failed to find block b-tree node")
			}

			blocks = append(blocks, blockBTreeNode)
		}
	case BlockTypeXXBlock:
		// XXBlock
		blockIdentifiers := make([]byte, entryCount*int(GetIdentifierSize(file.FormatType)))

		if _, err := blockReader.ReadAt(blockIdentifiers, 8); err != nil {
			return nil, eris.Wrap(err, "failed to read block identifiers")
		}

		for i := 0; i < entryCount; i++ {
			blockIdentifier := GetIdentifierFromBytes(blockIdentifiers[i*identifierSize:(i*identifierSize)+identifierSize], file.FormatType)
			blockBTreeNode, err := file.GetBlockBTreeNode(blockIdentifier) // TODO - Async then wait for the block b-tree node lookups.

			if err != nil {
				return nil, eris.Wrap(err, "failed to find block b-tree node")
			}

			// Recursive.
			blockBTreeNodeBlocks, err := file.GetBlocks(blockBTreeNode)

			if err != nil {
				return nil, eris.Wrap(err, "failed to get blocks")
			}

			blocks = append(blocks, blockBTreeNodeBlocks...)
		}
	default:
		return nil, ErrBlockTypeInvalid
	}

	return blocks, nil
}

// GetBlocksTotalSize returns the size of the external data referenced by the XBlock or XXBlock.
func (file *File) GetBlocksTotalSize(btreeNode BTreeNode) (uint32, error) {
	blockReader, err := file.GetBlockReader(btreeNode)

	if err != nil {
		return 0, eris.Wrap(err, "failed to get block reader")
	}

	blocksTotalSize := make([]byte, 4)

	if _, err := blockReader.ReadAt(blocksTotalSize, 4); err != nil {
		return 0, eris.Wrap(err, "failed to read total blocks size")
	}

	return binary.LittleEndian.Uint32(blocksTotalSize), nil
}
