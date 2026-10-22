package user_test

import (
	"context"
	"errors"
	"testing"

	"finance-ai-bot/internal/domain"
	"finance-ai-bot/internal/usecase/user"
)

// fakeRepo запоминает сохранённого пользователя и может вернуть ошибку.
type fakeRepo struct {
	saved domain.User
	users []domain.User
	err   error
}

func (r *fakeRepo) Upsert(_ context.Context, u domain.User) (domain.User, error) {
	r.saved = u
	return u, r.err
}

func (r *fakeRepo) List(context.Context) ([]domain.User, error) { return r.users, r.err }

func TestUpsertNormalizesNames(t *testing.T) {
	repo := &fakeRepo{}
	svc := user.NewService(repo)

	got, err := svc.Upsert(context.Background(), domain.User{
		TelegramID: 111, FirstName: "  Мама ", LastName: " Петрова ", Username: " @mama ",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := domain.User{TelegramID: 111, FirstName: "Мама", LastName: "Петрова", Username: "mama"}
	if got != want || repo.saved != want {
		t.Errorf("saved = %+v, want %+v", repo.saved, want)
	}
}

func TestUpsertRejectsBadTelegramID(t *testing.T) {
	for _, id := range []int64{0, -5} {
		repo := &fakeRepo{}
		_, err := user.NewService(repo).Upsert(context.Background(), domain.User{TelegramID: id, FirstName: "u"})
		var ve *domain.ValidationError
		if !errors.As(err, &ve) || ve.Field != "telegram_id" {
			t.Errorf("id %d: err = %v, want validation error on telegram_id", id, err)
		}
		if repo.saved != (domain.User{}) {
			t.Errorf("id %d: invalid user reached the repository", id)
		}
	}
}

func TestRepositoryErrorsArePropagated(t *testing.T) {
	boom := errors.New("db down")
	svc := user.NewService(&fakeRepo{err: boom})
	if _, err := svc.Upsert(context.Background(), domain.User{TelegramID: 1}); !errors.Is(err, boom) {
		t.Errorf("Upsert err = %v, want %v", err, boom)
	}
	if _, err := svc.List(context.Background()); !errors.Is(err, boom) {
		t.Errorf("List err = %v, want %v", err, boom)
	}
}

func TestList(t *testing.T) {
	users := []domain.User{{TelegramID: 1, FirstName: "a"}, {TelegramID: 2, FirstName: "b"}}
	got, err := user.NewService(&fakeRepo{users: users}).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].TelegramID != 2 {
		t.Errorf("List = %+v", got)
	}
}
