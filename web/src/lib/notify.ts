import { notifications } from '@mantine/notifications';
import { ApiError } from '../api';

export function notifySuccess(message: string) {
  notifications.show({ color: 'teal', message });
}

export function notifyWarning(message: string) {
  notifications.show({ color: 'yellow', message });
}

/** True for a 404 from the API, e.g. an expense that was deleted elsewhere. */
export function isNotFound(error: unknown): boolean {
  return error instanceof ApiError && error.status === 404;
}

export function notifyError(error: unknown, title = 'Ошибка') {
  const message = error instanceof Error ? error.message : 'Неизвестная ошибка';
  notifications.show({ color: 'red', title, message });
}

export type FieldErrors<F extends string> = Partial<Record<F, string>>;

/**
 * Routes a backend error to the matching form field when `error.field` is one
 * of `fields`; otherwise shows it as a notification.
 */
export function handleFormError<F extends string>(
  error: unknown,
  fields: readonly F[],
  setErrors: (errors: FieldErrors<F>) => void,
) {
  if (error instanceof ApiError && error.field && (fields as readonly string[]).includes(error.field)) {
    setErrors({ [error.field]: error.message } as FieldErrors<F>);
    return;
  }
  notifyError(error, 'Не удалось сохранить');
}
