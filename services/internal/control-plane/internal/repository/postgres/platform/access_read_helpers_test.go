package platform

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// TestAccessRowCollectionPolicies protects nil/capacity, partial scan failures and caller-owned
// iteration/Close boundaries; revision callers must still map scan failures to Unavailable.
// TestAccessRowCollectionPolicies проверяет nil/capacity, частичные scan failures и принадлежащие
// caller проверки iteration/Close; revision callers по-прежнему преобразуют scan error в Unavailable.
func TestAccessRowCollectionPolicies(t *testing.T) {
	sentinel := errors.New("synthetic protected scan failure")
	cases := []struct {
		name      string
		values    []int64
		fail      int
		iteration error
	}{
		{name: "empty"}, {name: "values", values: []int64{11, 12, 13}},
		{name: "first scan failure", values: []int64{11, 12}, fail: 1},
		{name: "partial scan failure", values: []int64{11, 12, 13}, fail: 2},
		{name: "iteration belongs to caller", values: []int64{11}, iteration: sentinel},
	}
	scan := func(row rowScanner) (int64, error) {
		var value int64
		if err := row.Scan(&value); err != nil {
			return value, sentinel
		}
		return value, nil
	}
	for _, reserved := range []bool{false, true} {
		for _, tc := range cases {
			t.Run(strconv.FormatBool(reserved)+"/"+tc.name, func(t *testing.T) {
				var trace []string
				rows := &revisionRowsFixture{values: tc.values, fail: tc.fail, iterationErr: tc.iteration, trace: &trace}
				var initial []int64
				if reserved {
					initial = make([]int64, 0, 4)
				}
				items, err := collectRowsInto(rows, initial, scan)
				if tc.fail > 0 {
					if err != sentinel || items != nil {
						t.Fatalf("scan error or partial result changed: err=%v rows=%v", err, items)
					}
				} else {
					if err != nil || len(items) != len(tc.values) {
						t.Fatalf("row collection changed: err=%v rows=%v", err, items)
					}
					for index, value := range tc.values {
						if items[index] != value {
							t.Fatal("row order changed")
						}
					}
					if len(tc.values) == 0 && (items == nil) != (!reserved) {
						t.Fatal("empty slice nil policy changed")
					}
					if reserved && cap(items) != 4 {
						t.Fatal("caller capacity was replaced")
					}
				}
				var expected []string
				count := len(tc.values)
				if tc.fail > 0 {
					count = tc.fail
				}
				for range count {
					expected = append(expected, "next", "scan")
				}
				if tc.fail == 0 {
					expected = append(expected, "next")
				}
				if !reflect.DeepEqual(trace, expected) || rows.closed {
					t.Fatalf("row ownership changed: trace=%v closed=%t", trace, rows.closed)
				}
			})
		}
	}
	var trace []string
	rows := &revisionRowsFixture{values: []int64{11}, fail: 1, trace: &trace}
	items, err := collectTypedRows(rows, 3, scan)
	if err != errs.ErrUnavailable || items != nil || rows.closed {
		t.Fatalf("revision error mapping changed: err=%v rows=%v", err, items)
	}
}

// TestRawAccessPageBoundaries checks lookahead cursor selection, one callback, nil state and
// backing-array reuse independently of database authorization or signed catalog cursors.
// TestRawAccessPageBoundaries проверяет cursor последнего видимого элемента, один callback,
// nil-состояние и общий backing array независимо от авторизации БД и подписанных catalog cursors.
func TestRawAccessPageBoundaries(t *testing.T) {
	cases := [][]int64{nil, {}, {11}, {11, 12}, {11, 12, 13}, {11, 12, 13, 14, 15}}
	for index, items := range cases {
		for _, limit := range []int32{1, 2, 3, 5} {
			t.Run(strconv.Itoa(index)+"/"+strconv.Itoa(int(limit)), func(t *testing.T) {
				calls := 0
				page, next := rawAccessPage(items, limit, func(value int64) string { calls++; return "ref-" + strconv.FormatInt(value, 10) })
				expectedCount := len(items)
				expectedNext := ""
				expectedCalls := 0
				if int32(expectedCount) > limit {
					expectedCount = int(limit)
					expectedNext = "ref-" + strconv.FormatInt(items[limit-1], 10)
					expectedCalls = 1
				}
				if len(page) != expectedCount || next != expectedNext || calls != expectedCalls {
					t.Fatalf("raw page changed: count=%d next=%q calls=%d", len(page), next, calls)
				}
				if (page == nil) != (items == nil) || cap(page) != cap(items) {
					t.Fatal("slice header policy changed")
				}
				if len(page) > 0 && &page[0] != &items[0] {
					t.Fatal("backing array changed")
				}
			})
		}
	}
	page, next := rawAccessPage[int64](nil, 1, nil)
	if page != nil || next != "" {
		t.Fatal("empty page changed")
	}
}

// TestAccessRoleQueryBoundaries checks the native rune/UTF-8/NUL and closed-alias limits,
// including valid duplicate aliases; normalization remains outside this predicate.
// TestAccessRoleQueryBoundaries проверяет прежние rune/UTF-8/NUL и closed-alias limits,
// включая допустимые повторные aliases; нормализация остаётся за пределами predicate.
func TestAccessRoleQueryBoundaries(t *testing.T) {
	alias := "i18n:SYSTEM_ROLE_OWNER"
	for _, tc := range []struct {
		name, text string
		aliases    []string
		want       bool
	}{
		{name: "empty", want: true}, {name: "unicode boundary", text: strings.Repeat("я", 200), want: true},
		{name: "unicode overflow", text: strings.Repeat("я", 201)}, {name: "invalid UTF8", text: string([]byte{255})},
		{name: "NUL", text: "x\x00"}, {name: "native newline", text: "x\ny", want: true},
		{name: "known alias", aliases: []string{alias}, want: true}, {name: "unknown alias", aliases: []string{"owner"}},
		{name: "ten duplicates", aliases: strings.Split(strings.TrimSuffix(strings.Repeat(alias+",", 10), ","), ","), want: true},
		{name: "eleven duplicates", aliases: strings.Split(strings.TrimSuffix(strings.Repeat(alias+",", 11), ","), ",")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := validAccessRoleQuery(tc.text, tc.aliases); got != tc.want {
				t.Fatalf("query guard changed: got=%t want=%t", got, tc.want)
			}
		})
	}
}

// TestReadAccessRowsPageFailures distinguishes the native access policy from revision pages:
// scan error identity passes through, iteration prevents cursor/commit, and failed Commit keeps
// the already-read page/cursor. It also checks nil results, exact context and cleanup ownership.
// TestReadAccessRowsPageFailures отличает access policy от revision pages: исходная scan error,
// отсутствие cursor/commit при iteration failure и сохранение прочитанной страницы/cursor при
// ошибке Commit. Также проверяет nil, точный context и владение очисткой.
func TestReadAccessRowsPageFailures(t *testing.T) {
	scanFailure := errors.New("synthetic access scan failure")
	commitFailure := errors.New("synthetic access commit failure")
	iterationFailure := errors.New("synthetic access iteration failure")
	for _, tc := range []struct {
		name                       string
		values, want               []int64
		fail                       int
		iteration, commit, wantErr error
		next                       string
		commits                    int
		trace                      []string
	}{
		{name: "empty", commits: 1, trace: []string{"next", "err", "commit"}},
		{name: "lookahead", values: []int64{8, 7, 6}, want: []int64{8, 7}, next: "7", commits: 1, trace: []string{"next", "scan", "next", "scan", "next", "scan", "next", "err", "cursor", "commit"}},
		{name: "partial scan", values: []int64{8, 7, 6}, fail: 2, wantErr: scanFailure, trace: []string{"next", "scan", "next", "scan"}},
		{name: "iteration", values: []int64{8, 7, 6}, iteration: iterationFailure, wantErr: errs.ErrUnavailable, trace: []string{"next", "scan", "next", "scan", "next", "scan", "next", "err"}},
		{name: "commit preserves page", values: []int64{8, 7, 6}, want: []int64{8, 7}, commit: commitFailure, wantErr: commitFailure, next: "7", commits: 1, trace: []string{"next", "scan", "next", "scan", "next", "scan", "next", "err", "cursor", "commit"}},
		{name: "cancelled commit", values: []int64{8}, want: []int64{8}, commit: context.Canceled, wantErr: context.Canceled, commits: 1, trace: []string{"next", "scan", "next", "err", "commit"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var trace []string
			rows := &revisionRowsFixture{values: tc.values, fail: tc.fail, iterationErr: tc.iteration, trace: &trace}
			tx := &revisionPageTxFixture{trace: &trace, err: tc.commit}
			ctx := t.Context()
			page, next, err := readAccessRowsPage(ctx, tx, rows, 2, func(row rowScanner) (revisionFixtureValue, error) {
				item, err := scanRevisionFixture(row)
				if err != nil {
					return item, scanFailure
				}
				return item, nil
			}, func(item revisionFixtureValue) string {
				trace = append(trace, "cursor")
				return strconv.FormatInt(item.revision, 10)
			})
			var got []int64
			for _, item := range page {
				got = append(got, item.revision)
			}
			if err != tc.wantErr || next != tc.next || !reflect.DeepEqual(got, tc.want) || (page == nil) != (tc.want == nil) {
				t.Fatalf("access page policy changed: page=%v next=%q err=%v", got, next, err)
			}
			if tx.calls != tc.commits || rows.closed || !reflect.DeepEqual(trace, tc.trace) {
				t.Fatalf("access page order changed: trace=%v commits=%d closed=%t", trace, tx.calls, rows.closed)
			}
			if tx.calls > 0 && tx.lastContext != ctx {
				t.Fatal("access commit context changed")
			}
		})
	}
}
