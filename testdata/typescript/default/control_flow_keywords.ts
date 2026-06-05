function controlFlowKeywords(input: unknown): number {
  let total = Number(input) || 0;

  for (let i = 0; i < 3; i++) {
    if (i === 1) {
      continue;
    } else if (i > 1 && total > 0) {
      break;
    } else {
      total += i;
    }
  }

  while (total < 10) {
    total++;
    if (total > 5 || input === undefined) {
      break;
    }
  }

  do {
    total--;
  } while (total > 6 && Boolean(input));

  switch (String(input ?? "missing")) {
    case "skip":
      return total;
    case "boom":
      throw new Error("boom");
    default:
      total += Math.max(total, 1);
  }

  try {
    const values = new Set([total]);
    return Array.from(values).length;
  } catch (error) {
    throw new Error(String(error));
  } finally {
    console.log("done");
  }
}
