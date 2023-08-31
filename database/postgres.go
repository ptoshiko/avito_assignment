package database

import (
	"context"
	"fmt"
	"httpserver/model"
	"log"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

type Database struct {
	conn *pgx.Conn
}

func New() (*Database, error) {

	dsn := "postgres://postgres:postgres@postgres:5432/postgres" + "?sslmode=disable"

	conn, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		log.Fatalf("Unable to connect to database: %v\n", err)
		//return nil, err
	}

	if err = conn.Ping(context.Background()); err != nil {
		log.Fatalf("can't ping db: %s", err)
		//return nil, err
	}

	return &Database{
		conn: conn,
	}, nil
}

func (db *Database) Close(ctx context.Context) {
	db.conn.Close(ctx)
}

func (db *Database) CreateSegment(ctx context.Context, seg model.SegName) error {
	tx, err := db.conn.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		// c.JSON(http.StatusInternalServerError, gin.H{"error": "Error starting transaction"})
		return err
	}
	defer tx.Rollback(ctx)

	var id int
	err = tx.QueryRow(ctx, `SELECT seg_id FROM segments WHERE seg_name = $1`, seg.SegName).Scan(&id)

	if err == nil {
		// c.JSON(http.StatusConflict, gin.H{"error": "Segment already exists", "id": id})
		return fmt.Errorf("Segment already exists")
	} else if err != pgx.ErrNoRows {
		// c.JSON(http.StatusInternalServerError, gin.H{"error": "Error while selecting segments"})
		return fmt.Errorf("Segment not found")
	}

	// Вставка нового сегмента
	_, err = tx.Exec(ctx, `INSERT INTO segments (seg_name) VALUES ($1)`, seg.SegName)
	if err != nil {
		// c.JSON(http.StatusInternalServerError, gin.H{"error": "Error inserting segment"})
		return err
	}

	err = tx.Commit(ctx)
	if err != nil {
		// c.JSON(http.StatusInternalServerError, gin.H{"error": "Error committing transaction"})
		return err
	}
	return nil
}

func (db *Database) DeleteSegment(ctx context.Context, seg model.SegName) error {
	tx, err := db.conn.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		// c.JSON(http.StatusInternalServerError, gin.H{"error": "Error starting transaction"})
		return err
	}
	defer tx.Rollback(ctx)

	var segmentID int
	err = tx.QueryRow(ctx, `SELECT seg_id FROM segments WHERE seg_name = $1`, seg.SegName).Scan(&segmentID)

	if err == pgx.ErrNoRows {
		// c.JSON(http.StatusNotFound, gin.H{"error": "Segment not found"})
		return fmt.Errorf("Segment not found")
	} else if err != nil {
		// c.JSON(http.StatusInternalServerError, gin.H{"error": "Error while selecting segments"})
		return err
	}

	_, err = tx.Exec(ctx, "DELETE FROM user_segment WHERE segment_id = $1", segmentID)
	if err != nil {
		// c.JSON(http.StatusInternalServerError, gin.H{"error": "Error deleting references in user_segment"})
		return err
	}

	_, err = tx.Exec(ctx, "DELETE FROM segments WHERE seg_name = $1", seg.SegName)
	if err != nil {
		// c.JSON(http.StatusInternalServerError, gin.H{"error": "Error deleting segment"})
		return err
	}

	err = tx.Commit(ctx)
	if err != nil {
		// c.JSON(http.StatusInternalServerError, gin.H{"error": "Error committing transaction"})
		return err
	}
	return nil
}

// вынести в utils
func intArrayToString(arr []int) string {
	values := make([]string, len(arr))
	for i, v := range arr {
		values[i] = strconv.Itoa(v)
	}
	return "{" + strings.Join(values, ",") + "}"
}

func (db *Database) UpdateUserSegments(ctx context.Context, us model.UserSegments) error {

	tx, err := db.conn.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		// c.JSON(http.StatusInternalServerError, gin.H{"error": "Error starting transaction"})
		return err
	}

	var segmentIDs []int
	if len(us.SegmentsToAdd) > 0 {
		rows, err := tx.Query(ctx, `
		SELECT seg_id
		FROM segments
		WHERE seg_name = ANY($1)
		`, us.SegmentsToAdd)

		if err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		// found := false
		for rows.Next() {
			// found = true
			var segmentID int
			if err := rows.Scan(&segmentID); err != nil {
				return err
			}
			segmentIDs = append(segmentIDs, segmentID)
		}
		rows.Close()

		if len(segmentIDs) != len(us.SegmentsToAdd) {
			return fmt.Errorf("Segment not found")
		}
	}

	// check if the segmnets are valid 
	// if len(us.SegmentsToRemove) > 0 {
	// 	rows, err := tx.Query(ctx, `
	// 		SELECT seg_id
	// 		FROM segments
	// 		WHERE seg_name = ANY($1)
	// 	`, us.SegmentsToRemove)

	if len(us.SegmentsToRemove) > 0 {
		// Check if the segments to remove exist in the user_segment table
		rows, err := tx.Query(ctx, `
			SELECT segment_id
			FROM user_segment
			WHERE user_id = $1 AND segment_id IN (
				SELECT seg_id
				FROM segments
				WHERE seg_name = ANY($2)
			)
		`, us.UserID, us.SegmentsToRemove)

		if err != nil {
			_ = tx.Rollback(ctx)
			return err
		}

		var deleteIDs []int
		// found = false
		for rows.Next() {
			// found = true 
			var deleteID int
			if err := rows.Scan(&deleteID); err != nil {
				return err
			}
			deleteIDs = append(deleteIDs, deleteID)
		}
		rows.Close()

		if len(deleteIDs) != len(us.SegmentsToRemove) {
			return fmt.Errorf("Segment not found")
		}
	}
	// insert segments 
	segmentIDsStr := intArrayToString(segmentIDs)
	_, err = tx.Exec(ctx, `
		INSERT INTO user_segment (user_id, segment_id)
		SELECT $1, unnest($2::integer[])
	`, us.UserID, segmentIDsStr)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}

	// // Remove segments from the user

	// if len(us.SegmentsToRemove) > 0 {
	query := `
		DELETE FROM user_segment
		WHERE user_id = $1 AND segment_id IN (
			SELECT seg_id FROM segments WHERE seg_name = ANY($2)
		)
	`
	_, err = tx.Exec(ctx, query, us.UserID, us.SegmentsToRemove)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
		// }
	}
	return tx.Commit(ctx)
}

func (db *Database) GetUserSegments(ctx context.Context, userID string) ([]model.Segment, error) {
	rows, err := db.conn.Query(ctx, `
		SELECT seg_id, seg_name
		FROM segments 
		INNER JOIN user_segment ON segments.seg_id = user_segment.segment_id
		WHERE user_segment.user_id = $1 
		`, userID)
	if err != nil {
		// c.JSON(http.StatusInternalServerError, gin.H{"error": "Error retrieving user segments"})
		return nil, err
	}


	var segments []model.Segment
	for rows.Next() {
		var seg model.Segment
		if err := rows.Scan(&seg.SegID, &seg.SegName); err != nil {
			// c.JSON(http.StatusInternalServerError, gin.H{"error": "Error scanning rows"})
			return nil, err
		}
		segments = append(segments, seg)
	}
	rows.Close()
	return segments, nil
}

