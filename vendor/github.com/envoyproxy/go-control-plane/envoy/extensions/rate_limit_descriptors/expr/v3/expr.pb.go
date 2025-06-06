// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.30.0
// 	protoc        v5.29.3
// source: envoy/extensions/rate_limit_descriptors/expr/v3/expr.proto

package exprv3

import (
	_ "github.com/cncf/xds/go/udpa/annotations"
	_ "github.com/envoyproxy/protoc-gen-validate/validate"
	v1alpha1 "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
	sync "sync"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

// The following descriptor entry is appended with a value computed
// from a symbolic Common Expression Language expression.
// See :ref:`attributes <arch_overview_attributes>` for the set of
// available attributes.
//
// .. code-block:: cpp
//
//	("<descriptor_key>", "<expression_value>")
type Descriptor struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	// The key to use in the descriptor entry.
	DescriptorKey string `protobuf:"bytes,1,opt,name=descriptor_key,json=descriptorKey,proto3" json:"descriptor_key,omitempty"`
	// If set to true, Envoy skips the descriptor if the expression evaluates to an error.
	// By default, the rate limit is not applied when an expression produces an error.
	SkipIfError bool `protobuf:"varint,2,opt,name=skip_if_error,json=skipIfError,proto3" json:"skip_if_error,omitempty"`
	// Types that are assignable to ExprSpecifier:
	//
	//	*Descriptor_Text
	//	*Descriptor_Parsed
	ExprSpecifier isDescriptor_ExprSpecifier `protobuf_oneof:"expr_specifier"`
}

func (x *Descriptor) Reset() {
	*x = Descriptor{}
	if protoimpl.UnsafeEnabled {
		mi := &file_envoy_extensions_rate_limit_descriptors_expr_v3_expr_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *Descriptor) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Descriptor) ProtoMessage() {}

func (x *Descriptor) ProtoReflect() protoreflect.Message {
	mi := &file_envoy_extensions_rate_limit_descriptors_expr_v3_expr_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Descriptor.ProtoReflect.Descriptor instead.
func (*Descriptor) Descriptor() ([]byte, []int) {
	return file_envoy_extensions_rate_limit_descriptors_expr_v3_expr_proto_rawDescGZIP(), []int{0}
}

func (x *Descriptor) GetDescriptorKey() string {
	if x != nil {
		return x.DescriptorKey
	}
	return ""
}

func (x *Descriptor) GetSkipIfError() bool {
	if x != nil {
		return x.SkipIfError
	}
	return false
}

func (m *Descriptor) GetExprSpecifier() isDescriptor_ExprSpecifier {
	if m != nil {
		return m.ExprSpecifier
	}
	return nil
}

func (x *Descriptor) GetText() string {
	if x, ok := x.GetExprSpecifier().(*Descriptor_Text); ok {
		return x.Text
	}
	return ""
}

func (x *Descriptor) GetParsed() *v1alpha1.Expr {
	if x, ok := x.GetExprSpecifier().(*Descriptor_Parsed); ok {
		return x.Parsed
	}
	return nil
}

type isDescriptor_ExprSpecifier interface {
	isDescriptor_ExprSpecifier()
}

type Descriptor_Text struct {
	// Expression in a text form, e.g. "connection.requested_server_name".
	Text string `protobuf:"bytes,3,opt,name=text,proto3,oneof"`
}

type Descriptor_Parsed struct {
	// Parsed expression in AST form.
	Parsed *v1alpha1.Expr `protobuf:"bytes,4,opt,name=parsed,proto3,oneof"`
}

func (*Descriptor_Text) isDescriptor_ExprSpecifier() {}

func (*Descriptor_Parsed) isDescriptor_ExprSpecifier() {}

var File_envoy_extensions_rate_limit_descriptors_expr_v3_expr_proto protoreflect.FileDescriptor

var file_envoy_extensions_rate_limit_descriptors_expr_v3_expr_proto_rawDesc = []byte{
	0x0a, 0x3a, 0x65, 0x6e, 0x76, 0x6f, 0x79, 0x2f, 0x65, 0x78, 0x74, 0x65, 0x6e, 0x73, 0x69, 0x6f,
	0x6e, 0x73, 0x2f, 0x72, 0x61, 0x74, 0x65, 0x5f, 0x6c, 0x69, 0x6d, 0x69, 0x74, 0x5f, 0x64, 0x65,
	0x73, 0x63, 0x72, 0x69, 0x70, 0x74, 0x6f, 0x72, 0x73, 0x2f, 0x65, 0x78, 0x70, 0x72, 0x2f, 0x76,
	0x33, 0x2f, 0x65, 0x78, 0x70, 0x72, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x12, 0x2f, 0x65, 0x6e,
	0x76, 0x6f, 0x79, 0x2e, 0x65, 0x78, 0x74, 0x65, 0x6e, 0x73, 0x69, 0x6f, 0x6e, 0x73, 0x2e, 0x72,
	0x61, 0x74, 0x65, 0x5f, 0x6c, 0x69, 0x6d, 0x69, 0x74, 0x5f, 0x64, 0x65, 0x73, 0x63, 0x72, 0x69,
	0x70, 0x74, 0x6f, 0x72, 0x73, 0x2e, 0x65, 0x78, 0x70, 0x72, 0x2e, 0x76, 0x33, 0x1a, 0x25, 0x67,
	0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x2f, 0x61, 0x70, 0x69, 0x2f, 0x65, 0x78, 0x70, 0x72, 0x2f, 0x76,
	0x31, 0x61, 0x6c, 0x70, 0x68, 0x61, 0x31, 0x2f, 0x73, 0x79, 0x6e, 0x74, 0x61, 0x78, 0x2e, 0x70,
	0x72, 0x6f, 0x74, 0x6f, 0x1a, 0x1d, 0x75, 0x64, 0x70, 0x61, 0x2f, 0x61, 0x6e, 0x6e, 0x6f, 0x74,
	0x61, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x2f, 0x73, 0x74, 0x61, 0x74, 0x75, 0x73, 0x2e, 0x70, 0x72,
	0x6f, 0x74, 0x6f, 0x1a, 0x17, 0x76, 0x61, 0x6c, 0x69, 0x64, 0x61, 0x74, 0x65, 0x2f, 0x76, 0x61,
	0x6c, 0x69, 0x64, 0x61, 0x74, 0x65, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x22, 0xcb, 0x01, 0x0a,
	0x0a, 0x44, 0x65, 0x73, 0x63, 0x72, 0x69, 0x70, 0x74, 0x6f, 0x72, 0x12, 0x2e, 0x0a, 0x0e, 0x64,
	0x65, 0x73, 0x63, 0x72, 0x69, 0x70, 0x74, 0x6f, 0x72, 0x5f, 0x6b, 0x65, 0x79, 0x18, 0x01, 0x20,
	0x01, 0x28, 0x09, 0x42, 0x07, 0xfa, 0x42, 0x04, 0x72, 0x02, 0x10, 0x01, 0x52, 0x0d, 0x64, 0x65,
	0x73, 0x63, 0x72, 0x69, 0x70, 0x74, 0x6f, 0x72, 0x4b, 0x65, 0x79, 0x12, 0x22, 0x0a, 0x0d, 0x73,
	0x6b, 0x69, 0x70, 0x5f, 0x69, 0x66, 0x5f, 0x65, 0x72, 0x72, 0x6f, 0x72, 0x18, 0x02, 0x20, 0x01,
	0x28, 0x08, 0x52, 0x0b, 0x73, 0x6b, 0x69, 0x70, 0x49, 0x66, 0x45, 0x72, 0x72, 0x6f, 0x72, 0x12,
	0x1d, 0x0a, 0x04, 0x74, 0x65, 0x78, 0x74, 0x18, 0x03, 0x20, 0x01, 0x28, 0x09, 0x42, 0x07, 0xfa,
	0x42, 0x04, 0x72, 0x02, 0x10, 0x01, 0x48, 0x00, 0x52, 0x04, 0x74, 0x65, 0x78, 0x74, 0x12, 0x38,
	0x0a, 0x06, 0x70, 0x61, 0x72, 0x73, 0x65, 0x64, 0x18, 0x04, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x1e,
	0x2e, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x2e, 0x61, 0x70, 0x69, 0x2e, 0x65, 0x78, 0x70, 0x72,
	0x2e, 0x76, 0x31, 0x61, 0x6c, 0x70, 0x68, 0x61, 0x31, 0x2e, 0x45, 0x78, 0x70, 0x72, 0x48, 0x00,
	0x52, 0x06, 0x70, 0x61, 0x72, 0x73, 0x65, 0x64, 0x42, 0x10, 0x0a, 0x0e, 0x65, 0x78, 0x70, 0x72,
	0x5f, 0x73, 0x70, 0x65, 0x63, 0x69, 0x66, 0x69, 0x65, 0x72, 0x42, 0xb3, 0x01, 0xba, 0x80, 0xc8,
	0xd1, 0x06, 0x02, 0x10, 0x02, 0x0a, 0x3d, 0x69, 0x6f, 0x2e, 0x65, 0x6e, 0x76, 0x6f, 0x79, 0x70,
	0x72, 0x6f, 0x78, 0x79, 0x2e, 0x65, 0x6e, 0x76, 0x6f, 0x79, 0x2e, 0x65, 0x78, 0x74, 0x65, 0x6e,
	0x73, 0x69, 0x6f, 0x6e, 0x73, 0x2e, 0x72, 0x61, 0x74, 0x65, 0x5f, 0x6c, 0x69, 0x6d, 0x69, 0x74,
	0x5f, 0x64, 0x65, 0x73, 0x63, 0x72, 0x69, 0x70, 0x74, 0x6f, 0x72, 0x73, 0x2e, 0x65, 0x78, 0x70,
	0x72, 0x2e, 0x76, 0x33, 0x42, 0x09, 0x45, 0x78, 0x70, 0x72, 0x50, 0x72, 0x6f, 0x74, 0x6f, 0x50,
	0x01, 0x5a, 0x5d, 0x67, 0x69, 0x74, 0x68, 0x75, 0x62, 0x2e, 0x63, 0x6f, 0x6d, 0x2f, 0x65, 0x6e,
	0x76, 0x6f, 0x79, 0x70, 0x72, 0x6f, 0x78, 0x79, 0x2f, 0x67, 0x6f, 0x2d, 0x63, 0x6f, 0x6e, 0x74,
	0x72, 0x6f, 0x6c, 0x2d, 0x70, 0x6c, 0x61, 0x6e, 0x65, 0x2f, 0x65, 0x6e, 0x76, 0x6f, 0x79, 0x2f,
	0x65, 0x78, 0x74, 0x65, 0x6e, 0x73, 0x69, 0x6f, 0x6e, 0x73, 0x2f, 0x72, 0x61, 0x74, 0x65, 0x5f,
	0x6c, 0x69, 0x6d, 0x69, 0x74, 0x5f, 0x64, 0x65, 0x73, 0x63, 0x72, 0x69, 0x70, 0x74, 0x6f, 0x72,
	0x73, 0x2f, 0x65, 0x78, 0x70, 0x72, 0x2f, 0x76, 0x33, 0x3b, 0x65, 0x78, 0x70, 0x72, 0x76, 0x33,
	0x62, 0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_envoy_extensions_rate_limit_descriptors_expr_v3_expr_proto_rawDescOnce sync.Once
	file_envoy_extensions_rate_limit_descriptors_expr_v3_expr_proto_rawDescData = file_envoy_extensions_rate_limit_descriptors_expr_v3_expr_proto_rawDesc
)

func file_envoy_extensions_rate_limit_descriptors_expr_v3_expr_proto_rawDescGZIP() []byte {
	file_envoy_extensions_rate_limit_descriptors_expr_v3_expr_proto_rawDescOnce.Do(func() {
		file_envoy_extensions_rate_limit_descriptors_expr_v3_expr_proto_rawDescData = protoimpl.X.CompressGZIP(file_envoy_extensions_rate_limit_descriptors_expr_v3_expr_proto_rawDescData)
	})
	return file_envoy_extensions_rate_limit_descriptors_expr_v3_expr_proto_rawDescData
}

var file_envoy_extensions_rate_limit_descriptors_expr_v3_expr_proto_msgTypes = make([]protoimpl.MessageInfo, 1)
var file_envoy_extensions_rate_limit_descriptors_expr_v3_expr_proto_goTypes = []interface{}{
	(*Descriptor)(nil),    // 0: envoy.extensions.rate_limit_descriptors.expr.v3.Descriptor
	(*v1alpha1.Expr)(nil), // 1: google.api.expr.v1alpha1.Expr
}
var file_envoy_extensions_rate_limit_descriptors_expr_v3_expr_proto_depIdxs = []int32{
	1, // 0: envoy.extensions.rate_limit_descriptors.expr.v3.Descriptor.parsed:type_name -> google.api.expr.v1alpha1.Expr
	1, // [1:1] is the sub-list for method output_type
	1, // [1:1] is the sub-list for method input_type
	1, // [1:1] is the sub-list for extension type_name
	1, // [1:1] is the sub-list for extension extendee
	0, // [0:1] is the sub-list for field type_name
}

func init() { file_envoy_extensions_rate_limit_descriptors_expr_v3_expr_proto_init() }
func file_envoy_extensions_rate_limit_descriptors_expr_v3_expr_proto_init() {
	if File_envoy_extensions_rate_limit_descriptors_expr_v3_expr_proto != nil {
		return
	}
	if !protoimpl.UnsafeEnabled {
		file_envoy_extensions_rate_limit_descriptors_expr_v3_expr_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*Descriptor); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
	}
	file_envoy_extensions_rate_limit_descriptors_expr_v3_expr_proto_msgTypes[0].OneofWrappers = []interface{}{
		(*Descriptor_Text)(nil),
		(*Descriptor_Parsed)(nil),
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_envoy_extensions_rate_limit_descriptors_expr_v3_expr_proto_rawDesc,
			NumEnums:      0,
			NumMessages:   1,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_envoy_extensions_rate_limit_descriptors_expr_v3_expr_proto_goTypes,
		DependencyIndexes: file_envoy_extensions_rate_limit_descriptors_expr_v3_expr_proto_depIdxs,
		MessageInfos:      file_envoy_extensions_rate_limit_descriptors_expr_v3_expr_proto_msgTypes,
	}.Build()
	File_envoy_extensions_rate_limit_descriptors_expr_v3_expr_proto = out.File
	file_envoy_extensions_rate_limit_descriptors_expr_v3_expr_proto_rawDesc = nil
	file_envoy_extensions_rate_limit_descriptors_expr_v3_expr_proto_goTypes = nil
	file_envoy_extensions_rate_limit_descriptors_expr_v3_expr_proto_depIdxs = nil
}
