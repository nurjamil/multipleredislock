package redislock_test

import (
	"context"
	"errors"
	"math/rand"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/nurjamil/multipleredislock"
	"github.com/redis/go-redis/v9"
)

var (
	lockKey  = "__bsm_redislock_unit_test____"
	lockKey2 = "key2_redislock"
)

var redisOpts = &redis.Options{
	Network: "tcp",
	Addr:    "127.0.0.1:6379",
}

func TestClient(t *testing.T) {
	ctx := context.Background()
	rc := redis.NewClient(redisOpts)
	defer teardown(t, rc)

	// init client
	client := New(rc)

	// obtain
	lock, err := client.Obtain(ctx, []string{lockKey, lockKey2}, time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release(ctx)

	if exp, got := 22, len(lock.Token()); exp != got {
		t.Fatalf("expected %v, got %v", exp, got)
	}

	// check TTL
	assertTTL(t, lock, time.Second)

	// try to obtain again
	_, err = client.Obtain(ctx, []string{lockKey}, time.Second, nil)
	if exp, got := ErrNotObtained, err; !errors.Is(got, exp) {
		t.Fatalf("expected %v, got %v", exp, got)
	}

	// manually unlock
	if err := lock.Release(ctx); err != nil {
		t.Fatal(err)
	}

	// lock again
	lock, err = client.Obtain(ctx, []string{lockKey}, time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release(ctx)
}

func TestObtain(t *testing.T) {
	ctx := context.Background()
	rc := redis.NewClient(redisOpts)
	defer teardown(t, rc)

	lock := quickObtain(t, rc, time.Second)
	if err := lock.Release(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestObtain_metadata(t *testing.T) {
	ctx := context.Background()
	rc := redis.NewClient(redisOpts)
	defer teardown(t, rc)

	meta := "my-data"
	lock, err := Obtain(ctx, rc, []string{lockKey}, time.Second, &Options{Metadata: meta})
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release(ctx)

	if exp, got := meta, lock.Metadata(); exp != got {
		t.Fatalf("expected %v, got %v", exp, got)
	}
}

func TestObtain_retry_success(t *testing.T) {
	ctx := context.Background()
	rc := redis.NewClient(redisOpts)
	defer teardown(t, rc)

	// obtain for 20ms
	lock1 := quickObtain(t, rc, 20*time.Millisecond)
	defer lock1.Release(ctx)

	// lock again with linar retry - 3x for 20ms
	lock2, err := Obtain(ctx, rc, []string{lockKey}, time.Second, &Options{
		RetryStrategy: LimitRetry(LinearBackoff(20*time.Millisecond), 3),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer lock2.Release(ctx)
}

func TestMultiple_Obtain_retry_success(t *testing.T) {
	ctx := context.Background()
	rc := redis.NewClient(redisOpts)
	defer teardown(t, rc)

	// obtain for 20ms
	lock1 := quickObtain(t, rc, 20*time.Millisecond)
	defer lock1.Release(ctx)

	// lock again with linar retry - 3x for 20ms
	lock2, err := Obtain(ctx, rc, []string{lockKey, lockKey2}, time.Second, &Options{
		RetryStrategy: LimitRetry(LinearBackoff(20*time.Millisecond), 3),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer lock2.Release(ctx)
}

func TestObtain_retry_failure(t *testing.T) {
	ctx := context.Background()
	rc := redis.NewClient(redisOpts)
	defer teardown(t, rc)

	// obtain for 50ms
	lock1 := quickObtain(t, rc, 50*time.Millisecond)
	defer lock1.Release(ctx)

	// lock again with linar retry - 2x for 5ms
	_, err := Obtain(ctx, rc, []string{lockKey}, time.Second, &Options{
		RetryStrategy: LimitRetry(LinearBackoff(10*time.Millisecond), 2),
	})

	if exp, got := ErrNotObtained, err; !errors.Is(got, exp) {
		t.Fatalf("expected %v, got %v", exp, got)
	}
}

func TestMultipleObtain_retry_failure(t *testing.T) {
	ctx := context.Background()
	rc := redis.NewClient(redisOpts)
	defer teardown(t, rc)

	// obtain for 50ms
	lock1 := quickObtain(t, rc, 50*time.Millisecond)
	defer lock1.Release(ctx)

	// lock again with linar retry - 2x for 5ms
	_, err := Obtain(ctx, rc, []string{lockKey, lockKey2}, time.Second, &Options{
		RetryStrategy: LimitRetry(LinearBackoff(10*time.Millisecond), 2),
	})
	if exp, got := ErrNotObtained, err; !errors.Is(got, exp) {
		t.Fatalf("expected %v, got %v", exp, got)
	}
}

func TestObtain_concurrent(t *testing.T) {
	ctx := context.Background()
	rc := redis.NewClient(redisOpts)
	defer teardown(t, rc)

	numLocks := int32(0)
	numThreads := 100
	wg := new(sync.WaitGroup)
	errs := make(chan error, numThreads)
	for i := 0; i < numThreads; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			wait := rand.Int63n(int64(10 * time.Millisecond))
			time.Sleep(time.Duration(wait))

			_, err := Obtain(ctx, rc, []string{lockKey}, time.Minute, nil)
			if err == ErrNotObtained {
				return
			} else if err != nil {
				errs <- err
			} else {
				atomic.AddInt32(&numLocks, 1)
			}
		}()
	}
	wg.Wait()

	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if exp, got := 1, int(numLocks); exp != got {
		t.Fatalf("expected %v, got %v", exp, got)
	}
}

func generateKeys(len int) []string {
	var res = make([]string, 0, len)
	for i := 0; i < len; i++ {
		res = append(res, "s"+strconv.Itoa(i))
	}

	return res
}
func TestMultiple_Obtain_concurrent(t *testing.T) {
	ctx := context.Background()
	rc := redis.NewClient(redisOpts)
	defer teardown(t, rc)

	numLocks := int32(0)
	numThreads := 100
	wg := new(sync.WaitGroup)
	errs := make(chan error, numThreads)
	keys := generateKeys(100)
	for i := 0; i < numThreads; i++ {
		wg.Add(5)

		go funcLockWithRandomizeOrder(wg, ctx, rc, errs, &numLocks, keys)
		go funcLockWithRandomizeOrder(wg, ctx, rc, errs, &numLocks, keys)
		go funcLockWithRandomizeOrder(wg, ctx, rc, errs, &numLocks, keys)
		go funcLockWithRandomizeOrder(wg, ctx, rc, errs, &numLocks, keys)
		go funcLockWithRandomizeOrder(wg, ctx, rc, errs, &numLocks, keys)
	}
	wg.Wait()

	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if exp, got := 1, int(numLocks); exp != got {
		t.Fatalf("expected %v, got %v", exp, got)
	}

	cleanup(t, rc, keys)
}

func cleanup(t *testing.T, rc *redis.Client, keys []string) {
	t.Helper()
	pipe := rc.Pipeline()
	for _, key := range keys {
		if err := pipe.Del(context.Background(), key).Err(); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if _, err := pipe.Exec(ctx); err != nil {
		t.Fatal(err)
	}
}

func funcLockWithRandomizeOrder(wg *sync.WaitGroup, ctx context.Context, rc *redis.Client, errs chan error, numLocks *int32, keys []string) bool {
	defer wg.Done()

	wait := rand.Int63n(int64(10 * time.Millisecond))
	time.Sleep(time.Duration(wait))

	newKeys := make([]string, len(keys))

	copy(newKeys, keys)

	lenKeys := len(newKeys)
	times := lenKeys
	for i := 0; i < times; i++ {
		randIdx := rand.Intn(lenKeys - 1)
		randIdx2 := rand.Intn(lenKeys - 1)
		newKeys[randIdx], newKeys[randIdx2] = newKeys[randIdx2], newKeys[randIdx]
	}

	_, err := Obtain(ctx, rc, newKeys, time.Second*10, &Options{
		RetryStrategy: LimitRetry(LinearBackoff(10*time.Millisecond), 10),
	})
	if err == ErrNotObtained {
		return true
	} else if err != nil {
		errs <- err
	} else {
		atomic.AddInt32(numLocks, 1)
	}
	return false
}

func TestLock_Refresh(t *testing.T) {
	ctx := context.Background()
	rc := redis.NewClient(redisOpts)
	defer teardown(t, rc)

	lock := quickObtain(t, rc, time.Second)
	defer lock.Release(ctx)

	// check TTL
	assertTTL(t, lock, time.Second)

	// update TTL
	if err := lock.Refresh(ctx, time.Minute); err != nil {
		t.Fatal(err)
	}

	// check TTL again
	assertTTL(t, lock, time.Minute)
}

func TestLock_Refresh_expired(t *testing.T) {
	ctx := context.Background()
	rc := redis.NewClient(redisOpts)
	defer teardown(t, rc)

	lock := quickObtain(t, rc, 5*time.Millisecond)
	defer lock.Release(ctx)

	// try releasing
	time.Sleep(10 * time.Millisecond)
	if exp, got := ErrNotObtained, lock.Refresh(ctx, time.Minute); !errors.Is(got, exp) {
		t.Fatalf("expected %v, got %v", exp, got)
	}
}

func TestLock_Release_expired(t *testing.T) {
	ctx := context.Background()
	rc := redis.NewClient(redisOpts)
	defer teardown(t, rc)

	lock := quickObtain(t, rc, 5*time.Millisecond)
	defer lock.Release(ctx)

	// try releasing
	time.Sleep(10 * time.Millisecond)
	if exp, got := ErrLockNotHeld, lock.Release(ctx); !errors.Is(got, exp) {
		t.Fatalf("expected %v, got %v", exp, got)
	}
}

func TestLock_Release_not_own(t *testing.T) {
	ctx := context.Background()
	rc := redis.NewClient(redisOpts)
	defer teardown(t, rc)

	lock := quickObtain(t, rc, time.Second)
	defer lock.Release(ctx)

	// manually transfer ownership
	if err := rc.Set(ctx, lockKey, "ABCD", 0).Err(); err != nil {
		t.Fatal(err)
	}

	// try releasing
	if exp, got := ErrLockNotHeld, lock.Release(ctx); !errors.Is(got, exp) {
		t.Fatalf("expected %v, got %v", exp, got)
	}
}

func quickObtain(t *testing.T, rc *redis.Client, ttl time.Duration) *Lock {
	t.Helper()

	lock, err := Obtain(context.Background(), rc, []string{lockKey, lockKey2}, ttl, nil)
	if err != nil {
		t.Fatal(err)
	}
	return lock
}

func assertTTL(t *testing.T, lock *Lock, exp time.Duration) {
	t.Helper()

	ttl, err := lock.TTL(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	delta := ttl - exp
	if delta < 0 {
		delta = 1 - delta
	}
	if delta > time.Second {
		t.Fatalf("expected ~%v, got %v", exp, ttl)
	}
}

func teardown(t *testing.T, rc *redis.Client) {
	t.Helper()

	if err := rc.Del(context.Background(), lockKey).Err(); err != nil {
		t.Fatal(err)
	}
	if err := rc.Del(context.Background(), lockKey2).Err(); err != nil {
		t.Fatal(err)
	}
	if err := rc.Close(); err != nil {
		t.Fatal(err)
	}
}
