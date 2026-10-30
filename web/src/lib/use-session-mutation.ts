import { useRef } from "react";
import { useMutation, type UseMutationOptions } from "@tanstack/react-query";
import { captureSession } from "./session-scope";

// A protected component belongs to the session in which it was mounted. Keep
// the normal mutation cache/variables, but never run its callbacks after logout.
export function useSessionMutation<
  TData = unknown,
  TError = Error,
  TVariables = void,
  TContext = unknown,
>(options: UseMutationOptions<TData, TError, TVariables, TContext>) {
  const scope = useRef(captureSession()).current;
  return useMutation(sessionMutationOptions(options, scope));
}

export function sessionMutationOptions<TData, TError, TVariables, TContext>(
  options: UseMutationOptions<TData, TError, TVariables, TContext>,
  scope = captureSession(),
): UseMutationOptions<TData, TError, TVariables, TContext> {
  return {
    ...options,
    mutationFn:
      options.mutationFn &&
      (async (...args) => {
        scope.assert();
        const result = await options.mutationFn!(...args);
        scope.assert();
        return result;
      }),
    onMutate: async (...args) => {
      scope.assert();
      const context = await options.onMutate?.(...args);
      scope.assert();
      return context as TContext;
    },
    onSuccess: (...args) =>
      scope.current() ? options.onSuccess?.(...args) : undefined,
    onError: (...args) =>
      scope.current() ? options.onError?.(...args) : undefined,
    onSettled: (...args) =>
      scope.current() ? options.onSettled?.(...args) : undefined,
  };
}
