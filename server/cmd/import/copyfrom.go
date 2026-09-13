// Package main (cmd/import) 的 pgx CopyFrom 行源辅助.
// pgx CopyFrom 需要实现 pgx.CopyFromSource 的迭代器; 这里把 [][]any 包装为一个.
package main

import (
	"github.com/jackc/pgx/v5"
)

// rowsSource 包装多行切片, 持有 idx 指针接收者内部前进步骤.
// 复用方式: 内部炸出新的 *rowsSource 即可, 避免多次调用污染位置.
type rowsSource struct {
	rows [][]any
	idx  int
}

func newRowsSource(rows [][]any) *rowsSource {
	return &rowsSource{rows: rows}
}

// Next 前进到下一行, 返回是否到达有效行. 类似 sql.Rows.Next().
func (r *rowsSource) Next() bool {
	if r.idx >= len(r.rows) {
		return false
	}
	r.idx++
	return r.idx <= len(r.rows)
}

// Values 返回当前行. 行索引 = idx-1 (Next 已经把 idx 移到下一位置).
func (r *rowsSource) Values() ([]any, error) {
	return r.rows[r.idx-1], nil
}

func (r *rowsSource) Err() error {
	return nil
}

var _ pgx.CopyFromSource = (*rowsSource)(nil)
