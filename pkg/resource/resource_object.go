// Licensed to Pulumi Corporation ("Pulumi") under one or more
// contributor license agreements.  See the NOTICE file distributed with
// this work for additional information regarding copyright ownership.
// Pulumi licenses this file to You under the Apache License, Version 2.0
// (the "License"); you may not use this file except in compliance with
// the License.  You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package resource

import (
	"reflect"

	"github.com/golang/glog"

	"github.com/pulumi/lumi/pkg/compiler/symbols"
	"github.com/pulumi/lumi/pkg/compiler/types"
	"github.com/pulumi/lumi/pkg/compiler/types/predef"
	"github.com/pulumi/lumi/pkg/eval/rt"
	"github.com/pulumi/lumi/pkg/tokens"
	"github.com/pulumi/lumi/pkg/util/contract"
)

// IsResourceObject returns true if the given runtime object is a
func IsResourceObject(obj *rt.Object) bool {
	return obj != nil && predef.IsResourceType(obj.Type())
}

// Object is a live resource object, connected to state that may change due to evaluation.
type Object struct {
	urn URN        // the resource's object urn, a human-friendly, unique name for the
	obj *rt.Object // the resource's live object reference.
}

var _ Resource = (*Object)(nil)

// NewObject creates a new resource object out of the runtime object provided.  The context is used to resolve
// dependencies between resources and must contain all references that could be encountered.
func NewObject(obj *rt.Object) *Object {
	contract.Assertf(IsResourceObject(obj), "Expected a resource type")
	return &Object{obj: obj}
}

func (r *Object) URN() URN          { return r.urn }
func (r *Object) Obj() *rt.Object   { return r.obj }
func (r *Object) Type() tokens.Type { return r.obj.Type().TypeToken() }

// ID fetches the object's ID.
func (r *Object) ID() ID {
	idobj := getIDObject(r.Obj())
	contract.Assert(idobj != nil)
	contract.Assert(idobj.IsString())
	return ID(idobj.StringValue())
}

// HasID returns true if the object already has an ID assigned to it.
func (r *Object) HasID() bool {
	idobj := getIDObject(r.Obj())
	return idobj != nil && !idobj.IsNull()
}

// SetID assigns an ID to the target object.  This must only happen once.
func (r *Object) SetID(id ID) {
	prop := r.obj.GetPropertyAddr(IDProperty, true, true)
	contract.Assertf(prop.Obj().IsNull() || prop.Obj().IsComputed(), "Unexpected double set on ID; previous=%v", prop)
	prop.Set(rt.NewStringObject(id.String()))
}

// getIDObject fetches the ID off the target object, dynamically, given its runtime value.
func getIDObject(obj *rt.Object) *rt.Object {
	contract.Assert(IsResourceObject(obj))
	if idprop := obj.GetPropertyAddr(IDProperty, false, false); idprop != nil {
		id := idprop.Obj()
		contract.Assert(id != nil)
		contract.Assert(id.IsString() || id.IsComputed())
		return id
	}
	return nil
}

// SetURN assignes a URN to the target object.  This must only happen once.
func (r *Object) SetURN(m URN) {
	contract.Requiref(!HasURN(r), "urn", "empty")
	r.urn = m
}

// Update updates the target object an ID and resource property map.  This mutates the live object connected to this
// resource and also archives the resource object's present state in the form of a state snapshot.
func (r *Object) Update(id ID, outputs PropertyMap) *State {
	contract.Require(HasURN(r), "urn")

	// First take a snapshot of the properties.
	inputs := r.CopyProperties()

	// Now assign the ID and copy everything in the property map, overwriting what exists.
	r.SetID(id)
	r.SetProperties(outputs)

	// Finally, return a state snapshot of the underlying object state.
	return NewState(r.Type(), r.URN(), id, inputs, outputs)
}

// CopyProperties creates a property map out of a resource's runtime object.  This is a snapshot and is completely
// disconnected from the object itself, such that any subsequent object updates will not be observed.
func (r *Object) CopyProperties() PropertyMap {
	resobj := r.Obj()
	return copyObject(resobj, resobj)
}

// SetProperties copies from a resource property map to the runtime object, overwriting properties as it goes.
func (r *Object) SetProperties(props PropertyMap) {
	contract.Assert(props != nil)
	setRuntimeProperties(r.Obj(), props)
}

func copyObject(resobj *rt.Object, obj *rt.Object) PropertyMap {
	contract.Assert(obj != nil)
	props := obj.PropertyValues()
	return copyObjectProperties(resobj, props)
}

// copyObjectProperty creates a single property value out of a runtime object.  It returns false if the property could
// not be stored in a property (e.g., it is a function or other unrecognized or unserializable runtime object).
func copyObjectProperty(resobj *rt.Object, obj *rt.Object) PropertyValue {
	t := obj.Type()

	if predef.IsResourceType(t) {
		// Resource references expand to that resource's ID.
		idobj := getIDObject(obj)
		if idobj != nil && idobj.IsString() {
			return NewStringProperty(idobj.StringValue())
		}

		// If an ID hasn't yet been assigned, we must be planning, and so this is a computed property.
		return MakeComputed(NewStringProperty(""))
	}

	// Serialize simple primitive types with their primitive equivalents.
	switch t {
	case types.Null:
		return NewNullProperty()
	case types.Bool:
		return NewBoolProperty(obj.BoolValue())
	case types.Number:
		return NewNumberProperty(obj.NumberValue())
	case types.String:
		return NewStringProperty(obj.StringValue())
	case types.Object, types.Dynamic:
		result := copyObject(nil, obj) // an object literal, clone it
		return NewObjectProperty(result)
	}

	// Serialize arrays, maps, and object instances in the obvious way.
	switch t.(type) {
	case *symbols.ArrayType:
		// Make a new array, clone each element, and return the result.
		var result []PropertyValue
		for _, e := range *obj.ArrayValue() {
			result = append(result, copyObjectProperty(nil, e.Obj()))
		}
		return NewArrayProperty(result)
	case *symbols.MapType:
		// Make a new map, clone each property value, and return the result.
		props := obj.PropertyValues()
		result := copyObjectProperties(nil, props)
		return NewObjectProperty(result)
	case *symbols.Class:
		// Make a new object that contains a deep clone of the source.
		result := copyObject(nil, obj)
		return NewObjectProperty(result)
	}

	// If a computed value, we can propagate an unknown value, but only for certain cases.
	if t.Computed() {
		// See if this is an output property.  An output property is a property that is set directly on the resource
		// object that is computed from precisely a single dependency and no expression.  Otherwise, it is computed.
		comp := obj.ComputedValue()
		var makeProperty func(PropertyValue) PropertyValue
		if !comp.Expr && len(comp.Sources) == 1 && comp.Sources[0] == resobj {
			makeProperty = MakeOutput
		} else {
			makeProperty = MakeComputed
		}

		// Now just wrap the underlying object appropriately.
		elem := t.(*symbols.ComputedType).Element
		switch elem {
		case types.Null:
			return makeProperty(NewNullProperty())
		case types.Bool:
			return makeProperty(NewBoolProperty(false))
		case types.Number:
			return makeProperty(NewNumberProperty(0))
		case types.String:
			return makeProperty(NewStringProperty(""))
		case types.Object, types.Dynamic:
			return makeProperty(NewObjectProperty(make(PropertyMap)))
		}
		switch elem.(type) {
		case *symbols.ArrayType:
			return makeProperty(NewArrayProperty(nil))
		case *symbols.Class:
			return makeProperty(NewObjectProperty(make(PropertyMap)))
		}
	}

	// We can safely skip serializing functions, however, anything else is unexpected at this point.
	_, isfunc := t.(*symbols.FunctionType)
	contract.Assertf(isfunc, "Unrecognized resource property object type '%v' (%v)", t, reflect.TypeOf(t))
	return PropertyValue{}
}

// copyObjectProperties copies a resource's properties.
func copyObjectProperties(resobj *rt.Object, props *rt.PropertyMap) PropertyMap {
	// Walk the object's properties and serialize them in a stable order.
	result := make(PropertyMap)
	for _, k := range props.Stable() {
		result[PropertyKey(k)] = copyObjectProperty(resobj, props.Get(k))
	}
	return result
}

// setRuntimeProperties translates from a resource property map into the equivalent runtime objects, and stores them on
// the given runtime object.
func setRuntimeProperties(obj *rt.Object, props PropertyMap) {
	for k, v := range props {
		glog.V(9).Infof("Setting resource object property: %v=%v", k, v)
		prop := obj.GetPropertyAddr(rt.PropertyKey(k), true, true)
		// TODO: we are only setting if IsNull == true, to avoid certain shortcomings in our serialization format
		//     today.  For example, if a resource ID appears, we must map it back to the runtime object.
		if prop.Obj().IsNull() {
			val := createRuntimeProperty(v)
			prop.Set(val)
		}
	}
}

// createRuntimeProperty translates a property value into a runtime object.
func createRuntimeProperty(v PropertyValue) *rt.Object {
	if v.IsNull() {
		return rt.Null
	} else if v.IsBool() {
		return rt.Bools[v.BoolValue()]
	} else if v.IsNumber() {
		return rt.NewNumberObject(v.NumberValue())
	} else if v.IsString() {
		return rt.NewStringObject(v.StringValue())
	} else if v.IsArray() {
		src := v.ArrayValue()
		arr := make([]*rt.Pointer, len(src))
		for i, elem := range src {
			ve := createRuntimeProperty(elem)
			arr[i] = rt.NewPointer(ve, false, nil, nil)
		}
		return rt.NewArrayObject(types.Dynamic, &arr)
	}

	contract.Assertf(v.IsObject(), "Expected an object, not a computed/output value")
	obj := rt.NewObject(types.Dynamic, nil, nil, nil)
	setRuntimeProperties(obj, v.ObjectValue())
	return obj
}