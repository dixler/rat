const n = 1;

function shadowing(n: number): number {
  let value = n;
  {
    const n = value + 1;
    const value = n;
    void value;
  }
  return value;
}

shadowing(n);
