package main

type item struct {
	value int
}

type itemList []item
type itemArray [2]itemList
type itemFunc func(*item) []item
type itemAlias = itemContainer

type itemSink interface {
	Send(item)
}

type itemContainer struct {
	items itemList
	done  chan item
	sink  itemSink
	fn    itemFunc
	value item
}

var globalPtr *item
var globalSlice []item
var globalMap map[string]item
var globalChan chan item
var globalNamed itemList
var globalArray itemArray
var globalSink itemSink
var globalFunc itemFunc
var globalContainer itemContainer
var globalAlias itemAlias
var globalValue item

func useReferenceTypes(ptr *item, slice []item, lookup map[string]item, updates chan item, named itemList, array itemArray, sink itemSink, fn itemFunc, container itemContainer, alias itemAlias, value item) {
	localPtr := ptr
	localSlice := slice
	localMap := lookup
	localChan := updates
	localNamed := named
	localArray := array
	localSink := sink
	localFunc := fn
	localContainer := container
	localAlias := alias
	localLiteral := itemContainer{items: named, done: updates, sink: sink, fn: fn, value: value}
	localValue := value

	globalPtr = localPtr
	globalSlice = localSlice
	globalMap = localMap
	globalChan = localChan
	globalNamed = localNamed
	globalArray = localArray
	globalSink = localSink
	globalFunc = localFunc
	globalContainer = localContainer
	globalAlias = localAlias
	globalContainer = localLiteral
	globalValue = localValue
}

func useValueArray(values [4]int) int {
	localValues := values
	return localValues[0]
}

func useReferenceArray(values [4]*int) *int {
	localValues := values
	return localValues[0]
}
