# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

go-pst is a Go library for reading Microsoft Outlook PST/OST/PAB files (the PFF format). Module path is `github.com/msilvoso/go-pst/v6`; the library lives in `pkg/` but the package name is `pst` (imported as `pst "github.com/msilvoso/go-pst/v6/pkg"`).

## Commands

```bash
go build ./...                                   # build the library
go test ./pkg/                                   # run all tests
go test ./pkg/ -run TestExample                  # end-to-end test: walks data/enron.pst
go test ./pkg/ -run TestGetBlockReaderCompressed # single test by name
```

- `TestExample` writes extracted attachments to `pkg/attachments/` — an untracked artifact, safe to delete.
- `cmd/properties/` is a **separate Go module** (own go.mod), not part of the root build. It's a one-off generator that downloads MS-OXPROPS and produces the `.proto`/`.pb.go` files in `pkg/properties/`. Rarely touched.
- The generated `pkg/properties/*.pb.go` files are not gofmt-clean; don't reformat them.

## Architecture

The code mirrors the three layers of the [MS-PST] spec (full spec in `docs/README.md`). Reading a property means descending through all three:

**NDB (Node Database) layer** — raw storage:
- `file.go` — `File` struct; parses the header: content type (PST/OST/PAB), format type (wVer 14/15 = ANSI 32-bit, 21/23 = Unicode 64-bit, 36 = Unicode 4k = OST 2013+), encryption type. Nearly every low-level function switches on `file.FormatType` for offsets/sizes.
- `btree.go`, `btree_store.go` — the node b-tree (node id → data/subnode block ids) and block b-tree (block id → file offset + size) are fully walked at open time into a `BTreeStore` (in-memory tidwall/btree by default; the interface exists so callers can persist them and skip the walk via `NewFromReaderWithBTrees`).
- `blocks.go` — XBlock/XXBlock trees that chain multi-block data, and `GetBlockReader`, the single chokepoint through which **all** block data is read. For the Unicode 4k (OST) format it transparently zlib-inflates compressed blocks (a block is compressed when `BTreeNode.Size != BTreeNode.InflatedSize`, both from the block b-tree leaf entry). Decompression happens before decryption.
- `local_descriptors.go` — subnode b-tree (per-node key→block mapping, used when data doesn't fit in the heap).
- `io_uring.go` (linux-only build tag) — `NewAsync` async reader; `DefaultReader` in file.go is the blocking fallback implementing the same `Reader` interface.

**LTP (Lists, Tables, Properties) layer** — structures on top of blocks:
- `heap_on_node.go`, `heap_on_node_reader.go` — Heap-on-Node (HN): concatenates a node's blocks into one logical reader; "compressible encryption" (permute cipher) is decoded here at read time.
- `btree_on_heap.go` — BTH, b-tree stored inside a heap.
- `property_context.go` (PC, table type 188) and `table_context.go` (TC, table type 124, row matrix) — the two structures every object's properties come from. `property_reader.go` decodes typed property values.

**Messaging layer** — semantics:
- `message_store.go`, `folder.go`, `message.go`, `attachment.go` — folders are walked via hierarchy/contents TCs; messages and attachments expose iterators. Embedded message attachments (`AttachMethod` 5) store a PtypObject subnode descriptor instead of binary data: `WriteTo` refuses them with `ErrAttachmentIsEmbeddedMessage`; open them with `Attachment.GetEmbeddedMessage` and recurse.
- `name_to_id_map.go` — named property resolution (node id 97).
- `properties.go` + `pkg/properties/` — properties are decoded into protobuf-generated structs (`properties.Message`, `properties.Appointment`, …) selected by message class; getters are generated, not hand-written.

**Conventions**: sentinel errors live in `errors.go` (prefixed `go-pst:`), wrapped with `eris.Wrap` for stack traces and matched with `eris.Is`. The OST 4k format is undocumented by Microsoft; the reference implementations are libpff, XstReader, and java-libpst (linked in README).

**Test data**: `data/enron.pst` (Unicode), `data/32-bit.pst` (ANSI), `data/support.pst`. There is no 4k OST test file, and none of the bundled files contain embedded message attachments (all attachments are `AttachMethod` 1, by-value).
