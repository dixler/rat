package main

type item struct {
	value int
}

type itemList []item

type itemSink interface {
	Send(item)
}

type itemContainer struct {
	items itemList
	done  chan item
	sink  itemSink
	value item
}

var globalPtr *item
var globalSlice []item
var globalMap map[string]item
var globalChan chan item
var globalNamed itemList
var globalSink itemSink
var globalContainer itemContainer
var globalArray [2]item
var globalValue item

func useReferenceTypes(ptr *item, slice []item, lookup map[string]item, updates chan item, named itemList, sink itemSink, container itemContainer, array [2]item, value item) {
	localPtr := ptr
	localSlice := slice
	localMap := lookup
	localChan := updates
	localNamed := named
	localSink := sink
	localContainer := container
	localLiteral := itemContainer{items: named, done: updates, sink: sink, value: value}
	localArray := array
	localValue := value

	globalPtr = localPtr
	globalSlice = localSlice
	globalMap = localMap
	globalChan = localChan
	globalNamed = localNamed
	globalSink = localSink
	globalContainer = localContainer
	globalContainer = localLiteral
	globalArray = localArray
	globalValue = localValue
}
