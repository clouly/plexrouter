import { useCallback, useEffect, useRef, useState } from "react";

export interface AsyncState<T> {
  data: T | undefined;
  error: Error | undefined;
  loading: boolean;
  reload: () => void;
  setData: (updater: T | ((prev: T | undefined) => T | undefined)) => void;
}

/**
 * Runs `fn` whenever `deps` change; ignores results of stale calls.
 * `reload()` re-runs it without clearing the current data (so tables don't flash).
 */
export function useAsync<T>(fn: () => Promise<T>, deps: unknown[]): AsyncState<T> {
  const [data, setDataState] = useState<T | undefined>(undefined);
  const [error, setError] = useState<Error | undefined>(undefined);
  const [loading, setLoading] = useState(true);
  const [tick, setTick] = useState(0);
  const callId = useRef(0);
  const fnRef = useRef(fn);
  fnRef.current = fn;

  useEffect(() => {
    const id = ++callId.current;
    setLoading(true);
    setError(undefined);
    fnRef
      .current()
      .then((d) => {
        if (id === callId.current) setDataState(d);
      })
      .catch((e: unknown) => {
        if (id === callId.current) setError(e instanceof Error ? e : new Error(String(e)));
      })
      .finally(() => {
        if (id === callId.current) setLoading(false);
      });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, tick]);

  const reload = useCallback(() => setTick((t) => t + 1), []);
  const setData = useCallback((u: T | ((prev: T | undefined) => T | undefined)) => {
    setDataState((prev) => (typeof u === "function" ? (u as (p: T | undefined) => T | undefined)(prev) : u));
  }, []);

  return { data, error, loading, reload, setData };
}

export function useDebounced<T>(value: T, ms = 350): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setV(value), ms);
    return () => clearTimeout(t);
  }, [value, ms]);
  return v;
}

export function useDocumentTitle(title: string, appName?: string) {
  useEffect(() => {
    const name = appName || "PlexRouter";
    document.title = title ? `${title} · ${name}` : name;
  }, [title, appName]);
}
