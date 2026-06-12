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
	"encoding/binary"
	"io"
	"strings"

	"github.com/msilvoso/go-pst/v6/pkg/properties"
	"github.com/rotisserie/eris"
)

// AttachMethodEmbeddedMessage (afEmbeddedMessage) indicates the attachment is an embedded message (.msg).
// References [MS-OXCMSG]: PidTagAttachMethod.
const AttachMethodEmbeddedMessage = 5

// Attachment represents a message attachment.
type Attachment struct {
	Identifier       Identifier
	PropertyContext  *PropertyContext
	LocalDescriptors []LocalDescriptor
	properties.Attachment
}

// HasAttachments returns true if this message has attachments.
func (message *Message) HasAttachments() (bool, error) {
	reader, err := message.PropertyContext.GetPropertyReader(3591, message.LocalDescriptors)

	if err != nil {
		return false, eris.Wrap(err, "failed to get property reader")
	}

	value, err := reader.GetInteger32()

	if err != nil {
		return false, eris.Wrap(err, "failed to read int32")
	}

	return value&0x10 != 0, nil
}

// GetAttachmentTableContext returns the table context of the attachments of this message.
// Note we only return the attachment identifier property.
func (message *Message) GetAttachmentTableContext() (*TableContext, error) {
	hasAttachments, err := message.HasAttachments()

	if err != nil {
		return nil, eris.Wrap(err, "failed to check if there are attachments")
	}

	if !hasAttachments {
		return nil, ErrAttachmentsNotFound
	}

	if message.AttachmentTableContext == nil {
		// Initialize the attachments table context.
		attachmentLocalDescriptor, err := FindLocalDescriptor(1649, message.LocalDescriptors)

		if err != nil {
			return nil, eris.Wrap(err, "failed to find attachment local descriptor")
		}

		attachmentHeapOnNode, err := message.File.GetHeapOnNodeFromLocalDescriptor(attachmentLocalDescriptor)

		if err != nil {
			return nil, eris.Wrap(err, "failed to get attachment Heap-on-Node")
		}

		attachmentLocalDescriptors, err := message.File.GetLocalDescriptorsFromIdentifier(attachmentLocalDescriptor.LocalDescriptorsIdentifier)

		if err != nil {
			return nil, eris.Wrap(err, "failed to get attachment local descriptors")
		}

		attachmentTableContext, err := message.File.GetTableContext(attachmentHeapOnNode, attachmentLocalDescriptors, 26610)

		if err != nil {
			return nil, eris.Wrap(err, "failed to get attachment table context")
		}

		message.AttachmentTableContext = &attachmentTableContext
	}

	return message.AttachmentTableContext, nil
}

// GetAttachmentCount returns the amount of rows in the attachment table context.
func (message *Message) GetAttachmentCount() (int, error) {
	attachmentTableContext, err := message.GetAttachmentTableContext()

	if eris.Is(err, ErrAttachmentsNotFound) {
		return 0, nil
	} else if err != nil {
		return 0, eris.Wrap(err, "failed to get attachment table context")
	}

	return len(attachmentTableContext.Properties), nil
}

// GetAttachment returns the specified attachment.
func (message *Message) GetAttachment(attachmentIndex int) (*Attachment, error) {
	attachmentsTableContext, err := message.GetAttachmentTableContext()

	if err != nil {
		return nil, eris.Wrap(err, "failed to get attachments table context")
	} else if attachmentIndex > len(attachmentsTableContext.Properties)-1 {
		return nil, ErrAttachmentIndexInvalid
	}

	var attachmentHNID Identifier

	for _, attachmentProperty := range attachmentsTableContext.Properties[attachmentIndex] {
		// We only get the attachment identifier property from GetAttachmentTableContext.
		propertyReader, err := attachmentsTableContext.GetPropertyReader(attachmentProperty)

		if err != nil {
			return nil, eris.Wrap(err, "failed to get attachments table context property reader")
		}

		identifier, err := propertyReader.GetInteger32()

		if err != nil {
			return nil, eris.Wrap(err, "failed to read identifier")
		}

		attachmentHNID = Identifier(identifier)
	}

	if attachmentHNID == 0 {
		return nil, eris.New("failed to get attachment HNID")
	}

	attachmentLocalDescriptor, err := FindLocalDescriptor(attachmentHNID, message.LocalDescriptors)

	if err != nil {
		return nil, eris.Wrap(err, "failed to find attachment local descriptor")
	}

	attachmentLocalDescriptors, err := message.File.GetLocalDescriptorsFromIdentifier(attachmentLocalDescriptor.LocalDescriptorsIdentifier)

	if err != nil {
		return nil, eris.Wrap(err, "failed to get local descriptors from identifier")
	}

	attachmentHeapOnNode, err := message.File.GetHeapOnNodeFromLocalDescriptor(attachmentLocalDescriptor)

	if err != nil {
		return nil, eris.Wrap(err, "failed to get attachment Heap-on-Node")
	}

	attachmentPropertyContext, err := message.File.GetPropertyContext(attachmentHeapOnNode)

	if err != nil {
		return nil, eris.Wrap(err, "failed to get attachment property context")
	}

	attachment := &Attachment{
		Identifier:       attachmentLocalDescriptor.Identifier,
		PropertyContext:  attachmentPropertyContext,
		LocalDescriptors: attachmentLocalDescriptors,
	}

	if err := attachmentPropertyContext.Populate(attachment, attachmentLocalDescriptors); err != nil {
		return nil, eris.Wrap(err, "failed to populate attachment property context")
	}

	return attachment, nil
}

// GetAttachment returns the attachment.
// Note that the properties aren't populated (call PropertyContext.Populate).
func (file *File) GetAttachment(messageIdentifier Identifier) (*Attachment, error) {
	attachmentsNode, err := file.GetNodeBTreeNode(messageIdentifier)

	if err != nil {
		return nil, eris.Wrap(err, "failed to find node b-tree node")
	}

	attachmentsDataNode, err := file.GetBlockBTreeNode(attachmentsNode.DataIdentifier)

	if err != nil {
		return nil, eris.Wrap(err, "failed to find block b-tree node")
	}

	attachmentsHeapOnNode, err := file.GetHeapOnNode(attachmentsDataNode)

	if err != nil {
		return nil, eris.Wrap(err, "failed to get Heap-on-Node")
	}

	localDescriptors, err := file.GetLocalDescriptors(attachmentsNode)

	if err != nil {
		return nil, eris.Wrap(err, "failed to find local descriptors")
	}

	propertyContext, err := file.GetPropertyContext(attachmentsHeapOnNode)

	attachment := &Attachment{
		Identifier:       messageIdentifier,
		PropertyContext:  propertyContext,
		LocalDescriptors: localDescriptors,
	}

	if err := propertyContext.Populate(attachment, localDescriptors); err != nil {
		return nil, eris.Wrap(err, "failed to populate attachment property context")
	}

	return attachment, nil
}

// GetAllAttachments returns the attachments of this message.
// See AttachmentIterator.
func (message *Message) GetAllAttachments() ([]*Attachment, error) {
	attachmentCount, err := message.GetAttachmentCount()

	if eris.Is(err, ErrAttachmentsNotFound) {
		return nil, nil
	} else if err != nil {
		return nil, eris.Wrap(err, "failed to get attachment count")
	}

	attachments := make([]*Attachment, attachmentCount)

	for i := 0; i < attachmentCount; i++ {
		attachment, err := message.GetAttachment(i)

		if err != nil {
			return nil, eris.Wrap(err, "failed to get attachment")
		}

		attachments[i] = attachment
	}

	return attachments, nil
}

// AttachmentIterator implements an attachment iterator.
type AttachmentIterator struct {
	message *Message

	err               error
	currentIndex      int
	currentAttachment *Attachment
}

// Err return the error cause.
func (attachmentIterator *AttachmentIterator) Err() error {
	return attachmentIterator.err
}

// Next will ensure that Value returns the next item when executed.
// If the next value is not retrievable, Next will return false and Err() will return the error cause.
func (attachmentIterator *AttachmentIterator) Next() bool {
	hasNext := len(attachmentIterator.message.AttachmentTableContext.Properties) > attachmentIterator.currentIndex

	if !hasNext {
		return false
	}

	attachment, err := attachmentIterator.message.GetAttachment(attachmentIterator.currentIndex)

	if err != nil {
		attachmentIterator.err = eris.Wrap(err, "failed to get attachment")
		return false
	}

	attachmentIterator.currentIndex++
	attachmentIterator.currentAttachment = attachment

	return true
}

// Value returns the current value in the iterator.
func (attachmentIterator *AttachmentIterator) Value() *Attachment {
	return attachmentIterator.currentAttachment
}

// Size returns the amount of attachments in the message iterator.
func (attachmentIterator *AttachmentIterator) Size() int {
	return len(attachmentIterator.message.AttachmentTableContext.Properties)
}

func (attachmentIterator *AttachmentIterator) CurrentIndex() int {
	return attachmentIterator.currentIndex
}

// GetAttachmentIterator returns an iterator for attachments.
func (message *Message) GetAttachmentIterator() (AttachmentIterator, error) {
	attachmentCount, err := message.GetAttachmentCount()

	// TODO - Return an empty iterator instead of an error.
	if err != nil {
		return AttachmentIterator{}, eris.Wrap(err, "failed to get attachment count")
	} else if attachmentCount == 0 {
		return AttachmentIterator{}, ErrAttachmentsNotFound
	}

	return AttachmentIterator{
		message: message,
	}, nil
}

// GetDisplayName returns the display name of this attachment (PidTagDisplayName).
// Email attachments (message/rfc822) are often stored with only a display name,
// without PidTagAttachLongFilename or PidTagAttachFilename.
// Returns an empty string if the property is absent.
func (attachment *Attachment) GetDisplayName() (string, error) {
	propertyReader, err := attachment.PropertyContext.GetPropertyReader(12289, attachment.LocalDescriptors)

	if eris.Is(err, ErrPropertyNotFound) || eris.Is(err, ErrPropertyNoData) {
		return "", nil
	} else if err != nil {
		return "", eris.Wrap(err, "failed to get display name property reader")
	}

	var displayName string

	if propertyReader.Property.Type == PropertyTypeString8 {
		// ANSI files store the display name as String8.
		displayName, err = propertyReader.GetString8(attachment.PropertyContext.File.CodePage)
	} else {
		displayName, err = propertyReader.GetString()
	}

	if eris.Is(err, ErrPropertyNoData) {
		return "", nil
	} else if err != nil {
		return "", eris.Wrap(err, "failed to read display name")
	}

	return displayName, nil
}

// GetFilename returns the best available filename for this attachment, trying
// PidTagAttachLongFilename, PidTagAttachFilename and PidTagDisplayName in that
// order. Email attachments (message/rfc822) usually carry only a display name;
// when the name has no extension one is appended from PidTagAttachExtension,
// or ".eml" for the message/rfc822 MIME tag.
// Returns an empty string if the attachment has no name at all.
func (attachment *Attachment) GetFilename() (string, error) {
	if filename := attachment.GetAttachLongFilename(); filename != "" {
		return filename, nil
	}

	if filename := attachment.GetAttachFilename(); filename != "" {
		return filename, nil
	}

	displayName, err := attachment.GetDisplayName()

	if err != nil || displayName == "" {
		return "", err
	}

	extension := attachment.GetAttachExtension()

	if extension == "" && strings.EqualFold(attachment.GetAttachMimeTag(), "message/rfc822") {
		extension = ".eml"
	}

	if extension != "" && !strings.HasSuffix(strings.ToLower(displayName), strings.ToLower(extension)) {
		displayName += extension
	}

	return displayName, nil
}

// IsEmbeddedMessage returns true if this attachment is an embedded message (.msg).
func (attachment *Attachment) IsEmbeddedMessage() bool {
	return attachment.GetAttachMethod() == AttachMethodEmbeddedMessage
}

// GetEmbeddedMessage returns the embedded message of this attachment, which can be
// walked like any other message (properties, recipients and nested attachments).
// The PtypObject value of PidTagAttachDataObject is the identifier of a subnode
// which is a fully formed message, except that it is not located in the NBT and
// has no parent folder.
// References "PtypObject Properties", "Attachment Data".
func (attachment *Attachment) GetEmbeddedMessage() (*Message, error) {
	if !attachment.IsEmbeddedMessage() {
		return nil, ErrAttachmentNotEmbeddedMessage
	}

	propertyReader, err := attachment.PropertyContext.GetPropertyReader(14081, attachment.LocalDescriptors)

	if err != nil {
		return nil, eris.Wrap(err, "failed to get attachment object property reader")
	}

	// The PtypObject heap allocation is the subnode identifier (Nid) followed by the total object size (ulSize).
	embeddedMessageIdentifier := make([]byte, 4)

	if _, err := propertyReader.ReadAt(embeddedMessageIdentifier, 0); err != nil {
		return nil, eris.Wrap(err, "failed to read embedded message identifier")
	}

	embeddedMessageLocalDescriptor, err := FindLocalDescriptor(Identifier(binary.LittleEndian.Uint32(embeddedMessageIdentifier)), attachment.LocalDescriptors)

	if err != nil {
		return nil, eris.Wrap(err, "failed to find embedded message local descriptor")
	}

	file := attachment.PropertyContext.File

	embeddedMessageHeapOnNode, err := file.GetHeapOnNodeFromLocalDescriptor(embeddedMessageLocalDescriptor)

	if err != nil {
		return nil, eris.Wrap(err, "failed to get embedded message Heap-on-Node")
	}

	embeddedMessageLocalDescriptors, err := file.GetLocalDescriptorsFromIdentifier(embeddedMessageLocalDescriptor.LocalDescriptorsIdentifier)

	if err != nil {
		return nil, eris.Wrap(err, "failed to get embedded message local descriptors")
	}

	embeddedMessagePropertyContext, err := file.GetPropertyContext(embeddedMessageHeapOnNode)

	if err != nil {
		return nil, eris.Wrap(err, "failed to get embedded message property context")
	}

	return file.newMessage(embeddedMessageLocalDescriptor.Identifier, embeddedMessagePropertyContext, embeddedMessageLocalDescriptors)
}

// WriteTo writes the attachment to the specified io.Writer.
// Returns ErrAttachmentIsEmbeddedMessage for embedded message attachments:
// their PidTagAttachDataObject value is a PtypObject subnode descriptor,
// not the attachment data (see GetEmbeddedMessage).
func (attachment *Attachment) WriteTo(writer io.Writer) (int64, error) {
	if attachment.IsEmbeddedMessage() {
		return 0, ErrAttachmentIsEmbeddedMessage
	}

	attachmentReader, err := attachment.PropertyContext.GetPropertyReader(14081, attachment.LocalDescriptors)

	if eris.Is(err, ErrPropertyNoData) {
		return 0, nil
	} else if err != nil {
		return 0, eris.Wrap(err, "failed to get attachment property reader")
	}

	sectionReader := io.NewSectionReader(&attachmentReader, 0, attachmentReader.Size())

	written, err := io.CopyN(writer, sectionReader, sectionReader.Size())

	if err != nil {
		return written, eris.Wrap(err, "failed to write attachment")
	}

	return written, nil
}
