interface Store {
  read(key: string): string;
}

function useStore(store: Store, key: string): string {
  const value = store.read(key);
  return value;
}

const memoryStore: Store = {
  read(key: string) {
    return key.toUpperCase();
  },
};

useStore(memoryStore, "demo");
