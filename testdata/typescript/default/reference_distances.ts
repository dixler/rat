const topLevel = 1;

function sameFileReference(): number {
  return topLevel;
}

function sameFunctionReference(input: number): number {
  const local = input + 1;
  return local + input;
}

function nestedFunctionReference(input: number): number {
  const outer = input;
  function inner(value: number): number {
    const local = value + outer;
    return local;
  }
  return inner(outer);
}

const builtinDistance = Promise.resolve(Map).then((ctor) => new ctor<string, number>());
