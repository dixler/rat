function nestedDeclarationForms(items: number[], input: unknown): number {
  let total = 0;

  if (items.length > 0) {
    const count = items.length;
    total += count;
  }

  for (let i = 0; i < items.length; i++) {
    total += items[i];
  }

  for (const [idx, value] of items.entries()) {
    total += idx + value;
  }

  const pair = { left: 1, right: 2 };
  const { left, right: renamedRight } = pair;
  total += left + renamedRight;

  const apply = (value: number): number => {
    const doubled = value * 2;
    return doubled;
  };

  total += apply(total);

  try {
    if (typeof input === "number") {
      total += input;
    }
  } catch (error) {
    total += String(error).length;
  }

  return total;
}
