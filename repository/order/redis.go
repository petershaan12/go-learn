package order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/petershaan12/go-learn/model"
	"github.com/redis/go-redis/v9"
)

type RedisRepo struct {
	Client *redis.Client
}

func orderIDKey(orderID uint64) string {
	return fmt.Sprintf("order:%d", orderID)
}

func (r *RedisRepo) Insert(ctx context.Context, order model.Order) error {
	data, err := json.Marshal(order)
	if err != nil {
		return fmt.Errorf("failed to marshal order: %w", err)
	}

	key := orderIDKey(order.OrderID)

	txn := r.Client.TxPipeline()

	res := txn.SetNX(ctx, key, string(data), 0)
	if err := res.Err(); err != nil {
		txn.Discard()
		return fmt.Errorf("failed to insert order into redis: %w", err)
	}

	if err := txn.SAdd(ctx, "orders", order.OrderID).Err(); err != nil {
		txn.Discard()
		return fmt.Errorf("failed to add order ID to orders set: %w", err)
	}

	return nil
}

var ErrNotExist = errors.New("order does not exist	")

func (r *RedisRepo) FindByID(ctx context.Context, id uint64) (model.Order, error) {
	key := orderIDKey(id)

	value, err := r.Client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return model.Order{}, ErrNotExist
	} else if err != nil {
		return model.Order{}, fmt.Errorf("get order from redis: %w", err)
	}

	var order model.Order
	err = json.Unmarshal([]byte(value), &order)
	if err != nil {
		return model.Order{}, fmt.Errorf("failed to unmarshal order: %w", err)
	}

	return order, nil
}

func (r *RedisRepo) DeleteByID(ctx context.Context, id uint64) error {
	key := orderIDKey(id)

	txn := r.Client.TxPipeline()
	delCmd := txn.Del(ctx, key)

	_, err := txn.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to exec: %w", err)
	}

	if delCmd.Val() == 0 {
		return ErrNotExist
	}

	return nil
}

func (r *RedisRepo) Update(ctx context.Context, order model.Order) error {
	data, err := json.Marshal(order)
	if err != nil {
		return fmt.Errorf("failed to marshal order: %w", err)
	}

	key := orderIDKey(order.OrderID)

	cmd := r.Client.SetXX(ctx, key, string(data), 0)
	if !cmd.Val() {
		return ErrNotExist
	}
	if err := cmd.Err(); err != nil {
		return fmt.Errorf("failed to update order in redis: %w", err)
	}
	return nil
}

type FindAllPage struct {
	Size   uint64
	Offset uint64
}

type FindResult struct {
	Orders []model.Order
	Cursor uint64
}

func (r *RedisRepo) FindAll(ctx context.Context, page FindAllPage) (FindResult, error) {
	res := r.Client.SScan(ctx, "orders", page.Offset, "*", int64(page.Size))

	keys, cursor, err := res.Result()
	if err != nil {
		return FindResult{}, fmt.Errorf("failed to scan orders set: %w", err)
	}

	if len(keys) == 0 {
		return FindResult{
			Orders: []model.Order{},
		}, nil
	}

	xs, err := r.Client.MGet(ctx, keys...).Result()
	if err != nil {
		return FindResult{}, fmt.Errorf("failed to mget orders: %w", err)
	}

	orders := make([]model.Order, len(xs))

	for i, x := range xs {
		x := x.(string)
		var order model.Order

		err := json.Unmarshal([]byte(x), &order)
		if err != nil {
			return FindResult{}, fmt.Errorf("failed to decode order json: %w", err)
		}

		orders[i] = order
	}

	return FindResult{
		Orders: orders,
		Cursor: cursor,
	}, nil
}
