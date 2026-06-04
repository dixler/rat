function controlFlowKeywords(): number {
  loop: while (true) {
    if (false) {
      continue;
    }
    break loop;
  }
  throw new Error("boom");
}
