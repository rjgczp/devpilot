import { useEffect, useRef, useState } from "react";
import { aborted, api, errorText, type Page } from "./api";

export function usePage<T>(
  path: string | null,
  identity: string,
  refresh: number,
) {
  const key = `${identity}:${path}:${refresh}`;
  const [state, setState] = useState<{
    key: string;
    items: T[];
    next: string | null;
    loading: boolean;
    error: string;
  }>({ key: "", items: [], next: null, loading: false, error: "" });
  const current = useRef<AbortController | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    current.current = controller;
    setState({ key, items: [], next: null, loading: !!path, error: "" });
    if (path)
      api<Page<T>>(`${path}?limit=50`, { signal: controller.signal })
        .then(({ body }) => {
          if (!controller.signal.aborted)
            setState({
              key,
              items: body.data,
              next: body.pagination.next_after,
              loading: false,
              error: "",
            });
        })
        .catch((error) => {
          if (!aborted(error) && !controller.signal.aborted)
            setState({
              key,
              items: [],
              next: null,
              loading: false,
              error: errorText(error),
            });
        });
    return () => controller.abort();
  }, [key, path]);
  async function more() {
    const controller = current.current;
    if (
      !path ||
      state.key !== key ||
      !state.next ||
      state.loading ||
      !controller
    )
      return;
    setState((old) => ({ ...old, loading: true }));
    try {
      const { body } = await api<Page<T>>(
        `${path}?limit=50&after=${encodeURIComponent(state.next)}`,
        { signal: controller.signal },
      );
      if (!controller.signal.aborted)
        setState((old) => ({
          key,
          items: [...old.items, ...body.data],
          next: body.pagination.next_after,
          loading: false,
          error: "",
        }));
    } catch (error) {
      if (!aborted(error) && !controller.signal.aborted)
        setState((old) => ({
          ...old,
          loading: false,
          error: errorText(error),
        }));
    }
  }
  return {
    ...(state.key === key
      ? state
      : { items: [] as T[], next: null, loading: !!path, error: "" }),
    more,
  };
}
