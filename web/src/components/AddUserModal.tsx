import { useState, type FormEvent } from 'react';
import { Alert, Button, Group, Modal, NumberInput, Stack, TextInput } from '@mantine/core';
import { useCreateUser, useUsers } from '../hooks/queries';
import { handleFormError, notifySuccess, type FieldErrors } from '../lib/notify';

const FIELDS = ['telegram_id', 'first_name', 'last_name', 'username'] as const;
type Field = (typeof FIELDS)[number];

interface Prefill {
  id: number;
  firstName: string;
  lastName: string;
  username: string;
}

interface Props {
  opened: boolean;
  onClose: () => void;
}

export function AddUserModal({ opened, onClose }: Props) {
  return (
    <Modal opened={opened} onClose={onClose} title="Добавить члена семьи" centered>
      <AddUserForm onDone={onClose} />
    </Modal>
  );
}

function AddUserForm({ onDone }: { onDone: () => void }) {
  const create = useCreateUser();
  const { data: users } = useUsers();
  const [telegramId, setTelegramId] = useState<number | string>('');
  const [firstName, setFirstName] = useState('');
  const [lastName, setLastName] = useState('');
  const [username, setUsername] = useState('');
  const [errors, setErrors] = useState<FieldErrors<Field>>({});

  // POST /users upserts, so an existing ID overwrites that member's data.
  const existing = telegramId !== '' ? users?.find((u) => u.telegram_id === Number(telegramId)) : undefined;

  // Prefill the member's current data once per matched ID so blank fields don't
  // wipe last_name/username on upsert; later edits by the user are kept.
  // Render-time state adjustment: each branch moves `prefilled` to a state that
  // no longer satisfies its own condition, so it cannot loop.
  const [prefilled, setPrefilled] = useState<Prefill | null>(null);
  if (existing && prefilled?.id !== existing.telegram_id) {
    const next: Prefill = {
      id: existing.telegram_id,
      firstName: existing.first_name,
      lastName: existing.last_name ?? '',
      username: existing.username ?? '',
    };
    // First match: fill only empty fields. Switching from another matched ID:
    // replace only values the user hasn't edited.
    const replace = (current: string, prev: string | undefined) =>
      prefilled === null ? current === '' : current === prev;
    if (replace(firstName, prefilled?.firstName)) setFirstName(next.firstName);
    if (replace(lastName, prefilled?.lastName)) setLastName(next.lastName);
    if (replace(username, prefilled?.username)) setUsername(next.username);
    setPrefilled(next);
  } else if (!existing && prefilled !== null) {
    // ID no longer matches: drop the other member's data, keep what the user typed.
    if (firstName === prefilled.firstName) setFirstName('');
    if (lastName === prefilled.lastName) setLastName('');
    if (username === prefilled.username) setUsername('');
    setPrefilled(null);
  }

  const handleSubmit = (event: FormEvent) => {
    event.preventDefault();
    const validation: FieldErrors<Field> = {};
    if (typeof telegramId !== 'number' || telegramId <= 0) validation.telegram_id = 'Укажите Telegram ID';
    if (!firstName.trim()) validation.first_name = 'Укажите имя';
    setErrors(validation);
    if (Object.keys(validation).length > 0) return;

    create.mutate(
      {
        telegram_id: telegramId as number,
        first_name: firstName.trim(),
        last_name: lastName.trim() || undefined,
        username: username.trim().replace(/^@/, '') || undefined,
      },
      {
        onSuccess: (user) => {
          notifySuccess(existing ? `Данные ${user.display_name} обновлены` : `${user.display_name} добавлен(а)`);
          onDone();
        },
        onError: (error) => handleFormError(error, FIELDS, setErrors),
      },
    );
  };

  return (
    <form onSubmit={handleSubmit} noValidate>
      <Stack gap="sm">
        <NumberInput
          label="Telegram ID"
          description="Числовой ID пользователя в Telegram"
          value={telegramId}
          onChange={setTelegramId}
          error={errors.telegram_id}
          allowDecimal={false}
          allowNegative={false}
          hideControls
          withAsterisk
          data-autofocus
        />
        {existing && (
          <Alert color="yellow" p="xs">
            Член семьи с таким ID уже есть — данные будут обновлены
          </Alert>
        )}
        <TextInput
          label="Имя"
          value={firstName}
          onChange={(e) => setFirstName(e.currentTarget.value)}
          error={errors.first_name}
          withAsterisk
        />
        <TextInput
          label="Фамилия"
          value={lastName}
          onChange={(e) => setLastName(e.currentTarget.value)}
          error={errors.last_name}
        />
        <TextInput
          label="Username"
          placeholder="@username"
          value={username}
          onChange={(e) => setUsername(e.currentTarget.value)}
          error={errors.username}
        />
        <Group justify="flex-end" mt="sm">
          <Button variant="default" onClick={onDone}>
            Отмена
          </Button>
          <Button type="submit" loading={create.isPending}>
            {existing ? 'Обновить' : 'Добавить'}
          </Button>
        </Group>
      </Stack>
    </form>
  );
}
