import { getRelayEnvironment } from "../../../shared/api";

export function invalidateProductQueries() {
  getRelayEnvironment().commitUpdate((store) => {
    store.getRoot().invalidateRecord();
  });
}
