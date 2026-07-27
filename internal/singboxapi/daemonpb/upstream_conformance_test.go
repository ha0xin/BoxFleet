package daemonpb

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// The vendored proto is a trimmed subset of upstream's, so `buf breaking`
// cannot police it: every deliberate omission would register as a breaking
// change and bury the one signal worth having. These tests are the substitute.
// They compare the vendored descriptor against the real compiled upstream
// descriptor, captured at the pinned tag and committed under testdata/, so the
// comparison is mechanical rather than a hand transcription somebody has to get
// right twice.
//
// Regenerate the fixture only when the pin moves — see README.md,
// "Re-verifying against a new sing-box tag".

const upstreamDescriptorSet = "testdata/upstream-" + upstreamTag + ".descriptorset.binpb"

// omittedFields are the only fields the vendored proto may drop. They are
// reserved rather than declared because upstream's buildConnectionProto never
// assigns them (daemon/started_service.go:977-997), so they decode as a
// constant zero.
var omittedFields = map[string]bool{
	"Connection.14": true, // uplink
	"Connection.15": true, // downlink
}

func TestVendoredFieldsMatchUpstream(t *testing.T) {
	upstream := loadUpstreamMessages(t)

	messages := File_daemonpb_singbox_daemon_connections_proto.Messages()
	if messages.Len() == 0 {
		t.Fatal("vendored file declares no messages")
	}

	seenOmissions := map[string]bool{}
	for index := range messages.Len() {
		message := messages.Get(index)
		name := string(message.Name())
		upstreamFields, ok := upstream[name]
		if !ok {
			t.Errorf("message %s does not exist upstream at %s", name, upstreamTag)
			continue
		}

		fields := message.Fields()
		for fieldIndex := range fields.Len() {
			field := fields.Get(fieldIndex)
			got := describeVendoredField(field)
			want, present := upstreamFields[int32(field.Number())]
			if !present {
				t.Errorf("%s field %d (%s) does not exist upstream", name, field.Number(), field.Name())
				continue
			}
			if got != want {
				t.Errorf("%s field %d:\n got  %s\n want %s", name, field.Number(), got, want)
			}
		}

		for _, number := range sortedNumbers(upstreamFields) {
			if fields.ByNumber(protoreflect.FieldNumber(number)) != nil {
				continue
			}
			key := fmt.Sprintf("%s.%d", name, number)
			if !omittedFields[key] {
				t.Errorf("%s is declared upstream (%s) but missing from the vendored proto; "+
					"an unintentional omission decodes as a zero value and silently loses data",
					key, upstreamFields[int32(number)])
				continue
			}
			seenOmissions[key] = true
		}
	}

	// An omission that stopped happening means upstream removed the field, and
	// the reservation and its rationale need revisiting.
	for key := range omittedFields {
		if !seenOmissions[key] {
			t.Errorf("%s is recorded as a deliberate omission but no longer exists upstream at %s", key, upstreamTag)
		}
	}
}

func TestVendoredEnumMatchesUpstream(t *testing.T) {
	// Enum numbers travel on the wire, so a reordering would silently relabel
	// every event.
	upstream := loadUpstreamEnums(t)

	enums := File_daemonpb_singbox_daemon_connections_proto.Enums()
	for index := range enums.Len() {
		enum := enums.Get(index)
		upstreamValues, ok := upstream[string(enum.Name())]
		if !ok {
			t.Errorf("enum %s does not exist upstream at %s", enum.Name(), upstreamTag)
			continue
		}
		values := enum.Values()
		if values.Len() != len(upstreamValues) {
			t.Errorf("enum %s has %d values, upstream has %d", enum.Name(), values.Len(), len(upstreamValues))
		}
		for valueIndex := range values.Len() {
			value := values.Get(valueIndex)
			want, present := upstreamValues[string(value.Name())]
			if !present {
				t.Errorf("enum value %s.%s does not exist upstream", enum.Name(), value.Name())
				continue
			}
			if int32(value.Number()) != want {
				t.Errorf("enum value %s.%s = %d, upstream has %d", enum.Name(), value.Name(), value.Number(), want)
			}
		}
	}
}

func TestVendoredMethodMatchesUpstream(t *testing.T) {
	// The one RPC BoxFleet vendors must exist upstream with the same name and
	// the same streaming mode; gRPC dispatches on the former and the transport
	// framing depends on the latter.
	upstreamMethod := findUpstreamMethod(t, "StartedService", "SubscribeConnections")

	method := File_daemonpb_singbox_daemon_connections_proto.Services().Get(0).Methods().Get(0)
	if got, want := method.IsStreamingClient(), upstreamMethod.GetClientStreaming(); got != want {
		t.Errorf("client streaming = %v, upstream has %v", got, want)
	}
	if got, want := method.IsStreamingServer(), upstreamMethod.GetServerStreaming(); got != want {
		t.Errorf("server streaming = %v, upstream has %v", got, want)
	}
	if got, want := "."+string(method.Input().FullName()), upstreamMethod.GetInputType(); got != want {
		t.Errorf("input type = %s, upstream has %s", got, want)
	}
	if got, want := "."+string(method.Output().FullName()), upstreamMethod.GetOutputType(); got != want {
		t.Errorf("output type = %s, upstream has %s", got, want)
	}
}

// --- upstream descriptor fixture --------------------------------------------

func loadUpstreamFile(t *testing.T) *descriptorpb.FileDescriptorProto {
	t.Helper()
	raw, err := os.ReadFile(filepath.FromSlash(upstreamDescriptorSet))
	if err != nil {
		t.Fatalf("read upstream descriptor set: %v", err)
	}
	set := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(raw, set); err != nil {
		t.Fatalf("unmarshal upstream descriptor set: %v", err)
	}
	for _, file := range set.GetFile() {
		if file.GetName() == "started_service.proto" && file.GetPackage() == "daemon" {
			return file
		}
	}
	t.Fatalf("upstream descriptor set has no daemon/started_service.proto")
	return nil
}

func loadUpstreamMessages(t *testing.T) map[string]map[int32]string {
	t.Helper()
	messages := map[string]map[int32]string{}
	for _, message := range loadUpstreamFile(t).GetMessageType() {
		fields := map[int32]string{}
		for _, field := range message.GetField() {
			fields[field.GetNumber()] = describeUpstreamField(field)
		}
		messages[message.GetName()] = fields
	}
	return messages
}

func loadUpstreamEnums(t *testing.T) map[string]map[string]int32 {
	t.Helper()
	enums := map[string]map[string]int32{}
	for _, enum := range loadUpstreamFile(t).GetEnumType() {
		values := map[string]int32{}
		for _, value := range enum.GetValue() {
			values[value.GetName()] = value.GetNumber()
		}
		enums[enum.GetName()] = values
	}
	return enums
}

func findUpstreamMethod(t *testing.T, service, method string) *descriptorpb.MethodDescriptorProto {
	t.Helper()
	for _, candidate := range loadUpstreamFile(t).GetService() {
		if candidate.GetName() != service {
			continue
		}
		for _, candidateMethod := range candidate.GetMethod() {
			if candidateMethod.GetName() == method {
				return candidateMethod
			}
		}
	}
	t.Fatalf("upstream has no %s.%s at %s", service, method, upstreamTag)
	return nil
}

// describeVendoredField and describeUpstreamField must render the same string
// for the same field. Both go through the descriptorpb enums so the two sides
// are formatted by identical code paths rather than by two spellings of the
// same idea.
func describeVendoredField(field protoreflect.FieldDescriptor) string {
	return formatField(
		string(field.Name()),
		descriptorpb.FieldDescriptorProto_Type(field.Kind()),
		descriptorpb.FieldDescriptorProto_Label(field.Cardinality()),
		qualifiedTypeName(field),
	)
}

func describeUpstreamField(field *descriptorpb.FieldDescriptorProto) string {
	return formatField(field.GetName(), field.GetType(), field.GetLabel(), field.GetTypeName())
}

func formatField(name string, kind descriptorpb.FieldDescriptorProto_Type, label descriptorpb.FieldDescriptorProto_Label, typeName string) string {
	return fmt.Sprintf("%s %s %s %s", name, kind, label, typeName)
}

// qualifiedTypeName mirrors the leading-dot form descriptorpb uses for message
// and enum references, and is empty for scalars.
func qualifiedTypeName(field protoreflect.FieldDescriptor) string {
	switch field.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return "." + string(field.Message().FullName())
	case protoreflect.EnumKind:
		return "." + string(field.Enum().FullName())
	default:
		return ""
	}
}

func sortedNumbers(fields map[int32]string) []int32 {
	numbers := make([]int32, 0, len(fields))
	for number := range fields {
		numbers = append(numbers, number)
	}
	sort.Slice(numbers, func(i, j int) bool { return numbers[i] < numbers[j] })
	return numbers
}
