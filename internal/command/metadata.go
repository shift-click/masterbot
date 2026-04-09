package command

import "github.com/shift-click/masterbot/internal/commandmeta"

type descriptorSupport struct {
	descriptor commandmeta.Descriptor
}

func newDescriptorSupport(id string) descriptorSupport {
	return descriptorSupport{descriptor: commandmeta.Must(id)}
}

func (s descriptorSupport) Descriptor() commandmeta.Descriptor {
	return s.descriptor
}

func (s descriptorSupport) Name() string {
	return s.descriptor.Name
}

func (s descriptorSupport) Aliases() []string {
	return append([]string(nil), s.descriptor.SlashAliases...)
}

func (s descriptorSupport) Description() string {
	return s.descriptor.Description
}
