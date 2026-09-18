package clientconfig

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Inserts and visibility changes are recorded separately from provider updates.
// Values are SQLite text/numeric/null scalars; all identifiers are allow-listed.
type allRepairCatalog struct {
	Host, ID      string
	Insert        bool
	BeforeMissing sql.NullInt64
	Values        map[string]any
	Rollout       repairFile
}

type allRepairRollout struct {
	file allRepairDiskFile
	ids  map[string]bool
}

var repairCatalogColumns = []string{"host_id", "thread_id", "display_title", "source_created_at", "source_updated_at", "cwd", "source_kind", "model_provider", "observation_sequence", "source_detail", "missing_candidate", "git_branch", "thread_source", "archived"}

func collectRepairCatalog(directory, target string, nonRoot map[string]bool, rollouts map[string]allRepairRollout) ([]allRepairCatalog, error) {
	path := filepath.Join(directory, codexStateDatabaseFilename)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	db, err := openCodexHistoryDatabaseMode(path, "ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	columns, err := allRepairColumns(ctx, db, "local_thread_catalog")
	if err != nil {
		return nil, err
	}
	for _, name := range repairCatalogColumns[:9] {
		if !columns[name] {
			return nil, nil
		}
	}
	threads, err := allRepairColumns(ctx, db, "threads")
	if err != nil {
		return nil, err
	}
	if !threads["id"] || !threads["rollout_path"] {
		return nil, nil
	}
	hostColumns, err := allRepairColumns(ctx, db, "local_thread_catalog_hosts")
	if err != nil {
		return nil, err
	}
	host := "local"
	if len(hostColumns) > 0 {
		if !hostColumns["host_id"] || !hostColumns["host_kind"] {
			return nil, nil
		}
		var count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*), COALESCE(MIN(host_id),'') FROM local_thread_catalog_hosts WHERE LOWER(COALESCE(host_kind,''))='local'").Scan(&count, &host); err != nil {
			return nil, err
		}
		if count != 1 || host == "" {
			return nil, nil
		}
	}
	textExpr := func(name, fallback string) string {
		if threads[name] {
			return "COALESCE(" + name + "," + fallback + ")"
		}
		return fallback
	}
	timeExpr := func(name string) string {
		if threads[name+"_ms"] {
			return "COALESCE(" + name + "_ms,0)/1000.0"
		}
		if threads[name] {
			return "CASE WHEN " + name + ">9999999999 THEN " + name + "/1000.0 ELSE COALESCE(" + name + ",0) END"
		}
		return "0"
	}
	title := "id"
	for _, name := range []string{"first_user_message", "preview", "title", "name"} {
		if threads[name] {
			title = "COALESCE(NULLIF(" + name + ",'')," + title + ")"
		}
	}
	query := "SELECT id," + title + "," + timeExpr("created_at") + "," + timeExpr("updated_at") + "," + textExpr("cwd", "''") + "," + textExpr("source", "'cli'") + ",rollout_path," + textExpr("git_branch", "NULL") + "," + textExpr("thread_source", "NULL") + "," + textExpr("archived", "0") + "," + textExpr("has_user_event", "1") + "," + textExpr("agent_role", "''") + " FROM threads ORDER BY id"
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	var candidates []allRepairCatalog
	for rows.Next() {
		var id, name, cwd, source, rollout, role string
		var created, updated float64
		var branch, threadSource sql.NullString
		var archived, userEvent int
		if err := rows.Scan(&id, &name, &created, &updated, &cwd, &source, &rollout, &branch, &threadSource, &archived, &userEvent, &role); err != nil {
			rows.Close()
			return nil, err
		}
		if id == "" || nonRoot[id] || archived != 0 || userEvent != 1 || role != "" || strings.EqualFold(source, "exec") || allRepairNonRoot(source) || threadSource.Valid && threadSource.String != "" && !strings.EqualFold(threadSource.String, "user") {
			continue
		}
		// Only resurrect a list entry when its actual local rollout still exists.
		if rollout == "" {
			continue
		}
		resolved := rollout
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(directory, resolved)
		}
		rel, err := filepath.Rel(directory, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			continue
		}
		if strings.HasPrefix(rel, "archived_sessions"+string(os.PathSeparator)) {
			continue
		}
		observed, ok := rollouts[filepath.Clean(resolved)]
		if !ok || !observed.ids[id] {
			continue
		}
		if err := repairSafePath(resolved); err != nil {
			rows.Close()
			return nil, err
		}
		info, err := os.Stat(resolved)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			rows.Close()
			return nil, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		values := map[string]any{"host_id": host, "thread_id": id, "display_title": name, "source_created_at": created, "source_updated_at": updated, "cwd": cwd, "source_kind": source, "source_detail": rollout, "model_provider": target, "missing_candidate": int64(0), "git_branch": nil, "thread_source": nil, "archived": int64(0)}
		if branch.Valid {
			values["git_branch"] = branch.String
		}
		if threadSource.Valid {
			values["thread_source"] = threadSource.String
		}
		for key := range values {
			if !columns[key] {
				delete(values, key)
			}
		}
		candidates = append(candidates, allRepairCatalog{Host: host, ID: id, Values: values, Rollout: repairFile{rel, observed.file.before, observed.file.after, uint32(observed.file.mode)}})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	var sequence int64
	if err := db.QueryRowContext(ctx, "SELECT COALESCE(MAX(observation_sequence),0) FROM local_thread_catalog WHERE host_id=?", host).Scan(&sequence); err != nil {
		return nil, err
	}
	var result []allRepairCatalog
	for _, row := range candidates {
		expr := "NULL"
		if columns["missing_candidate"] {
			expr = "missing_candidate"
		}
		err := db.QueryRowContext(ctx, "SELECT "+expr+" FROM local_thread_catalog WHERE host_id=? AND thread_id=?", row.Host, row.ID).Scan(&row.BeforeMissing)
		if errors.Is(err, sql.ErrNoRows) {
			sequence++
			row.Insert = true
			row.Values["observation_sequence"] = sequence
			result = append(result, row)
		} else if err != nil {
			return nil, err
		} else if row.BeforeMissing.Valid && row.BeforeMissing.Int64 != 0 {
			row.Values = nil
			result = append(result, row)
		}
	}
	return result, nil
}

func validateRepairCatalogRollouts(directory string, rows []allRepairCatalog) error {
	for _, row := range rows {
		path, err := repairInside(directory, row.Rollout.Relative)
		if err != nil {
			return err
		}
		file := allRepairDiskFile{path: path, mode: os.FileMode(row.Rollout.Mode)}
		// Files changed by this repair are now After; a catalog-only repair is Before.
		if historyFileMatches(file, row.Rollout.After) == nil {
			continue
		}
		if err := historyFileMatches(file, row.Rollout.Before); err != nil {
			return err
		}
	}
	return nil
}

func updateRepairCatalog(ctx context.Context, tx *sql.Tx, repairs []allRepairCatalog, restore bool) error {
	allowed := map[string]bool{}
	for _, name := range repairCatalogColumns {
		allowed[name] = true
	}
	seen := map[string]bool{}
	for _, row := range repairs {
		key := row.Host + "\x00" + row.ID
		if row.Host == "" || row.ID == "" || seen[key] {
			return errors.New("无效或重复的会话目录恢复键")
		}
		seen[key] = true
		if row.Insert {
			if row.Values["host_id"] != row.Host || row.Values["thread_id"] != row.ID {
				return errors.New("无效的会话目录备份")
			}
			var names []string
			for name := range row.Values {
				if !allowed[name] {
					return errors.New("不支持的会话目录字段")
				}
				names = append(names, name)
			}
			sort.Strings(names)
			var args []any
			var marks, clauses []string
			for _, name := range names {
				value := row.Values[name]
				if number, ok := value.(json.Number); ok {
					if integer, err := number.Int64(); err == nil {
						value = integer
					} else if real, err := number.Float64(); err == nil {
						value = real
					} else {
						return errors.New("无效的会话目录数值")
					}
				}
				args = append(args, value)
				marks = append(marks, "?")
				clauses = append(clauses, name+" IS ?")
			}
			if !restore {
				if _, err := tx.ExecContext(ctx, "INSERT INTO local_thread_catalog ("+strings.Join(names, ",")+") VALUES ("+strings.Join(marks, ",")+")", args...); err != nil {
					return err
				}
			} else {
				var count int
				if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM local_thread_catalog WHERE host_id=? AND thread_id=?", row.Host, row.ID).Scan(&count); err != nil {
					return err
				}
				if count == 0 {
					continue
				}
				if count != 1 {
					return errors.New("会话目录主键不唯一")
				}
				result, err := tx.ExecContext(ctx, "DELETE FROM local_thread_catalog WHERE "+strings.Join(clauses, " AND "), args...)
				if err != nil {
					return err
				}
				n, err := result.RowsAffected()
				if err != nil {
					return err
				}
				if n != 1 {
					return errors.New("新建的会话目录记录已修改，拒绝撤销")
				}
			}
		} else {
			if !row.BeforeMissing.Valid || row.BeforeMissing.Int64 == 0 {
				return errors.New("无效的会话可见性备份")
			}
			before, after := row.BeforeMissing.Int64, int64(0)
			if restore {
				before, after = after, before
			}
			var current int64
			if err := tx.QueryRowContext(ctx, "SELECT missing_candidate FROM local_thread_catalog WHERE host_id=? AND thread_id=?", row.Host, row.ID).Scan(&current); err != nil {
				return err
			}
			if restore && current == after {
				continue
			}
			if current != before {
				return errors.New("会话可见性标记已修改，拒绝覆盖")
			}
			result, err := tx.ExecContext(ctx, "UPDATE local_thread_catalog SET missing_candidate=? WHERE host_id=? AND thread_id=? AND missing_candidate=?", after, row.Host, row.ID, before)
			if err != nil {
				return err
			}
			n, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if n != 1 {
				return errors.New("会话目录候选已变化")
			}
		}
	}
	return nil
}

func bumpRepairCatalogRevision(ctx context.Context, tx *sql.Tx) error {
	var exists int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_table_info('local_thread_catalog_metadata') WHERE name='catalog_revision'").Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return nil
	}
	result, err := tx.ExecContext(ctx, "UPDATE local_thread_catalog_metadata SET catalog_revision=COALESCE(catalog_revision,0)+1")
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		var hasID int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_table_info('local_thread_catalog_metadata') WHERE name='id'").Scan(&hasID); err != nil {
			return err
		}
		if hasID > 0 {
			_, err = tx.ExecContext(ctx, "INSERT INTO local_thread_catalog_metadata(id,catalog_revision) VALUES(1,1)")
			return err
		}
	}
	return nil
}
