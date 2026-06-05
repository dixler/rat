import React, { useState as useLocalState } from "react";
import * as helpers from "./helpers";

interface Reader {
  read(key: string): string;
}

class MemoryReader implements Reader {
  prefix = "memory";

  read(key: string): string {
    const value = helpers.format(key);
    return `${this.prefix}:${value}`;
  }
}

function useReader(reader: Reader, key: string): string {
  const [value, setValue] = useLocalState(reader.read(key));
  setValue(value);
  return React.createElement("span", null, value).props.children;
}

const reader = new MemoryReader();
useReader(reader, "demo");
