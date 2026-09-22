package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/db"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage/internal/gen"
	"modernc.org/sqlite"
)

type Location = gen.Location

var ErrLocationParentNotFound = errors.New("storage: parent location not found in this group")

var ErrLocationCycle = errors.New("storage: move would create a cycle in the location tree")

const sqliteConstraintForeignKeyCode = 787

func isForeignKeyConstraintViolation(err error) bool {
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) {
		return false
	}
	return sqliteErr.Code() == sqliteConstraintForeignKeyCode
}

type CreateLocationParams struct {
	ID       string
	Name     string
	ParentID string
	Now      int64
}

func (p CreateLocationParams) validate() error {
	switch {
	case p.ID == "":
		return errors.New("storage: CreateLocationParams: ID is empty")
	case p.Name == "":
		return errors.New("storage: CreateLocationParams: Name is empty")
	case p.Now <= 0:
		return errors.New("storage: CreateLocationParams: Now must be a positive Unix-millisecond timestamp")
	}
	return nil
}

type UpdateLocationParams struct {
	LocationID      string
	Name            string
	ParentID        string
	ExpectedVersion int64
	Now             int64
}

func (p UpdateLocationParams) validate() error {
	switch {
	case p.LocationID == "":
		return errors.New("storage: UpdateLocationParams: LocationID is empty")
	case p.Name == "":
		return errors.New("storage: UpdateLocationParams: Name is empty")
	case p.ExpectedVersion <= 0:
		return errors.New("storage: UpdateLocationParams: ExpectedVersion must be positive")
	case p.Now <= 0:
		return errors.New("storage: UpdateLocationParams: Now must be a positive Unix-millisecond timestamp")
	}
	return nil
}

var ErrLocationNotEmpty = errors.New("storage: location still holds live children or items")

var ErrLocationReassignTarget = errors.New("storage: reassignment target is the location being deleted or one of its descendants")

type LocationNotEmptyError struct {
	LocationID string
	ChildCount int64
	ItemCount  int64
}

func (e *LocationNotEmptyError) Error() string {
	return fmt.Sprintf("storage: location %q holds %d live child location(s) and %d live item(s); pass a reassignment target to delete it anyway",
		e.LocationID, e.ChildCount, e.ItemCount)
}

func (e *LocationNotEmptyError) Unwrap() error { return ErrLocationNotEmpty }

type DeleteLocationParams struct {
	LocationID string

	Reassign bool

	ReassignTo string

	Now int64
}

func (p DeleteLocationParams) validate() error {
	switch {
	case p.LocationID == "":
		return errors.New("storage: DeleteLocationParams: LocationID is empty")
	case p.Now <= 0:
		return errors.New("storage: DeleteLocationParams: Now must be a positive Unix-millisecond timestamp")
	case !p.Reassign && p.ReassignTo != "":
		return errors.New("storage: DeleteLocationParams: ReassignTo is set but Reassign is false; set Reassign to use it")
	}
	return nil
}

type LocationRepository interface {
	Get(ctx context.Context, locationID string) (Location, error)

	List(ctx context.Context) ([]Location, error)

	Create(ctx context.Context, p CreateLocationParams) (Location, error)

	Update(ctx context.Context, p UpdateLocationParams) (Location, error)

	Delete(ctx context.Context, p DeleteLocationParams) error

	Descendants(ctx context.Context, locationID string) ([]string, error)

	Tree(ctx context.Context) ([]*LocationNode, error)
}

type locationRepository struct {
	binding
}

func (r locationRepository) Get(ctx context.Context, locationID string) (Location, error) {
	loc, err := r.queries().GetLocation(ctx, gen.GetLocationParams{
		GroupID:    r.group(),
		LocationID: locationID,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Location{}, fmt.Errorf("storage: location %q: %w", locationID, ErrNotFound)
	case err != nil:
		return Location{}, fmt.Errorf("storage: get location %q: %w", locationID, err)
	}
	return loc, nil
}

func (r locationRepository) List(ctx context.Context) ([]Location, error) {
	rows, err := r.queries().ListLocations(ctx, r.group())
	if err != nil {
		return nil, fmt.Errorf("storage: list locations for group %q: %w", r.group(), err)
	}
	return rows, nil
}

func (r locationRepository) Create(ctx context.Context, p CreateLocationParams) (Location, error) {
	if err := p.validate(); err != nil {
		return Location{}, err
	}

	var created Location
	err := r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		row, err := createLocationTx(ctx, tx, q, r.group(), p)
		if err != nil {
			return err
		}
		created = row
		return nil
	})
	if err != nil {
		return Location{}, err
	}
	return created, nil
}

func createLocationTx(ctx context.Context, tx *sql.Tx, q *gen.Queries, group string, p CreateLocationParams) (Location, error) {
	if p.ParentID != "" {
		if _, err := q.GetLocation(ctx, gen.GetLocationParams{GroupID: group, LocationID: p.ParentID}); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Location{}, fmt.Errorf("storage: create location: parent %q: %w", p.ParentID, ErrLocationParentNotFound)
			}
			return Location{}, fmt.Errorf("storage: create location: get parent %q: %w", p.ParentID, err)
		}
	}

	seq, err := db.AllocChangeSeq(ctx, tx, group)
	if err != nil {
		return Location{}, fmt.Errorf("storage: allocate change_seq for location: %w", err)
	}

	if err := q.CreateLocation(ctx, gen.CreateLocationParams{
		ID:        p.ID,
		GroupID:   group,
		Name:      p.Name,
		ParentID:  nullString(p.ParentID),
		Now:       p.Now,
		ChangeSeq: seq,
	}); err != nil {
		if isForeignKeyConstraintViolation(err) {
			return Location{}, fmt.Errorf("storage: create location: parent %q: %w", p.ParentID, ErrLocationParentNotFound)
		}
		return Location{}, fmt.Errorf("storage: create location: %w", err)
	}

	created, err := q.GetLocation(ctx, gen.GetLocationParams{GroupID: group, LocationID: p.ID})
	if err != nil {
		return Location{}, fmt.Errorf("storage: read back created location %q: %w", p.ID, err)
	}
	return created, nil
}

func (r locationRepository) Update(ctx context.Context, p UpdateLocationParams) (Location, error) {
	if err := p.validate(); err != nil {
		return Location{}, err
	}

	var updated Location
	err := r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		row, err := updateLocationTx(ctx, tx, q, r.group(), p)
		if err != nil {
			return err
		}
		updated = row
		return nil
	})
	if err != nil {
		return Location{}, err
	}
	return updated, nil
}

func updateLocationTx(ctx context.Context, tx *sql.Tx, q *gen.Queries, group string, p UpdateLocationParams) (Location, error) {
	current, err := q.GetLocation(ctx, gen.GetLocationParams{GroupID: group, LocationID: p.LocationID})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Location{}, fmt.Errorf("storage: location %q: %w", p.LocationID, ErrNotFound)
	case err != nil:
		return Location{}, fmt.Errorf("storage: get location %q for update: %w", p.LocationID, err)
	}
	if current.Version != p.ExpectedVersion {
		return Location{}, ErrVersionMismatch
	}

	if p.ParentID != "" {
		if _, err := q.GetLocation(ctx, gen.GetLocationParams{GroupID: group, LocationID: p.ParentID}); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Location{}, fmt.Errorf("storage: update location %q: parent %q: %w", p.LocationID, p.ParentID, ErrLocationParentNotFound)
			}
			return Location{}, fmt.Errorf("storage: update location %q: get parent %q: %w", p.LocationID, p.ParentID, err)
		}

		isCycle, err := locationMoveCreatesCycle(ctx, q, group, p.LocationID, p.ParentID)
		if err != nil {
			return Location{}, fmt.Errorf("storage: update location %q: cycle check: %w", p.LocationID, err)
		}
		if isCycle {
			return Location{}, fmt.Errorf("storage: update location %q: parent %q: %w", p.LocationID, p.ParentID, ErrLocationCycle)
		}
	}

	seq, err := db.AllocChangeSeq(ctx, tx, group)
	if err != nil {
		return Location{}, fmt.Errorf("storage: allocate change_seq for location update: %w", err)
	}

	rows, err := q.UpdateLocation(ctx, gen.UpdateLocationParams{
		Name:            p.Name,
		ParentID:        nullString(p.ParentID),
		Now:             p.Now,
		ChangeSeq:       seq,
		GroupID:         group,
		LocationID:      p.LocationID,
		ExpectedVersion: p.ExpectedVersion,
	})
	if err != nil {
		if isForeignKeyConstraintViolation(err) {
			return Location{}, fmt.Errorf("storage: update location %q: parent %q: %w", p.LocationID, p.ParentID, ErrLocationParentNotFound)
		}
		return Location{}, fmt.Errorf("storage: update location %q: %w", p.LocationID, err)
	}
	if rows != 1 {
		return Location{}, fmt.Errorf("storage: update location %q: matched %d rows, want 1 (pre-check read version %d); this should be unreachable under this server's single-writer transaction model",
			p.LocationID, rows, current.Version)
	}

	updated, err := q.GetLocation(ctx, gen.GetLocationParams{GroupID: group, LocationID: p.LocationID})
	if err != nil {
		return Location{}, fmt.Errorf("storage: read back updated location %q: %w", p.LocationID, err)
	}
	return updated, nil
}

func locationMoveCreatesCycle(ctx context.Context, q *gen.Queries, groupID, movingID, candidateParentID string) (bool, error) {
	edges, err := q.ListLocationParentEdges(ctx, groupID)
	if err != nil {
		return false, fmt.Errorf("list location parent edges: %w", err)
	}

	parentOf := make(map[string]string, len(edges))
	for _, e := range edges {
		if e.ParentID.Valid {
			parentOf[e.ID] = e.ParentID.String
		}
	}

	current := candidateParentID
	for i := 0; i <= len(edges); i++ {
		if current == movingID {
			return true, nil
		}
		next, ok := parentOf[current]
		if !ok {
			return false, nil
		}
		current = next
	}
	return false, fmt.Errorf("location parent-edge walk from %q did not terminate within %d steps -- the stored graph may already contain a cycle this method did not create", candidateParentID, len(edges)+1)
}

func (r locationRepository) Delete(ctx context.Context, p DeleteLocationParams) error {
	if err := p.validate(); err != nil {
		return err
	}

	return r.writeTx(ctx, func(tx *sql.Tx, q *gen.Queries) error {
		if _, err := q.GetLocation(ctx, gen.GetLocationParams{GroupID: r.group(), LocationID: p.LocationID}); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("storage: location %q: %w", p.LocationID, ErrNotFound)
			}
			return fmt.Errorf("storage: get location %q for delete: %w", p.LocationID, err)
		}

		childCount, err := q.CountLiveChildLocations(ctx, gen.CountLiveChildLocationsParams{
			GroupID:  r.group(),
			ParentID: sql.NullString{String: p.LocationID, Valid: true},
		})
		if err != nil {
			return fmt.Errorf("storage: count child locations of %q: %w", p.LocationID, err)
		}
		itemCount, err := q.CountLiveItemsInLocation(ctx, gen.CountLiveItemsInLocationParams{
			GroupID:    r.group(),
			LocationID: sql.NullString{String: p.LocationID, Valid: true},
		})
		if err != nil {
			return fmt.Errorf("storage: count items in %q: %w", p.LocationID, err)
		}

		if !p.Reassign && (childCount > 0 || itemCount > 0) {
			return &LocationNotEmptyError{LocationID: p.LocationID, ChildCount: childCount, ItemCount: itemCount}
		}

		if p.Reassign && p.ReassignTo != "" {
			if _, err := q.GetLocation(ctx, gen.GetLocationParams{GroupID: r.group(), LocationID: p.ReassignTo}); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return fmt.Errorf("storage: delete location %q: reassign target %q: %w", p.LocationID, p.ReassignTo, ErrLocationParentNotFound)
				}
				return fmt.Errorf("storage: delete location %q: get reassign target %q: %w", p.LocationID, p.ReassignTo, err)
			}

			inSubtree, err := locationMoveCreatesCycle(ctx, q, r.group(), p.LocationID, p.ReassignTo)
			if err != nil {
				return fmt.Errorf("storage: delete location %q: reassign target check: %w", p.LocationID, err)
			}
			if inSubtree {
				return fmt.Errorf("storage: delete location %q: reassign target %q: %w", p.LocationID, p.ReassignTo, ErrLocationReassignTarget)
			}
		}

		seq, err := db.AllocChangeSeq(ctx, tx, r.group())
		if err != nil {
			return fmt.Errorf("storage: allocate change_seq for location delete: %w", err)
		}

		if p.Reassign {
			target := nullString(p.ReassignTo)

			moved, err := q.ReparentLiveChildren(ctx, gen.ReparentLiveChildrenParams{
				NewParentID: target,
				Now:         p.Now,
				ChangeSeq:   seq,
				GroupID:     r.group(),
				OldParentID: sql.NullString{String: p.LocationID, Valid: true},
			})
			if err != nil {
				return fmt.Errorf("storage: reparent children of %q: %w", p.LocationID, err)
			}
			if moved != childCount {
				return fmt.Errorf("storage: reparent children of %q: moved %d rows, counted %d; the count and the move disagree about which rows this location holds",
					p.LocationID, moved, childCount)
			}

			movedItems, err := q.MoveLiveItemsToLocation(ctx, gen.MoveLiveItemsToLocationParams{
				NewLocationID: target,
				Now:           p.Now,
				ChangeSeq:     seq,
				GroupID:       r.group(),
				OldLocationID: sql.NullString{String: p.LocationID, Valid: true},
			})
			if err != nil {
				return fmt.Errorf("storage: move items out of %q: %w", p.LocationID, err)
			}
			if movedItems != itemCount {
				return fmt.Errorf("storage: move items out of %q: moved %d rows, counted %d; the count and the move disagree about which rows this location holds",
					p.LocationID, movedItems, itemCount)
			}
		}

		rows, err := q.DeleteLocation(ctx, gen.DeleteLocationParams{
			DeletedAt:  sql.NullInt64{Int64: p.Now, Valid: true},
			Now:        p.Now,
			ChangeSeq:  seq,
			GroupID:    r.group(),
			LocationID: p.LocationID,
		})
		if err != nil {
			return fmt.Errorf("storage: delete location %q: %w", p.LocationID, err)
		}
		switch {
		case rows == 0:
			return fmt.Errorf("storage: location %q: %w", p.LocationID, ErrNotFound)
		case rows != 1:
			return fmt.Errorf("storage: delete location %q: matched %d rows, want 1; this should be unreachable given ux_locations_group_id_id's uniqueness on (group_id, id) -- see this method's own doc for why the guard is written this way regardless",
				p.LocationID, rows)
		}
		return nil
	})
}

func (r locationRepository) Descendants(ctx context.Context, locationID string) ([]string, error) {
	if locationID == "" {
		return nil, errors.New("storage: Descendants: locationID is empty")
	}

	edges, err := r.queries().ListLocationParentEdges(ctx, r.group())
	if err != nil {
		return nil, fmt.Errorf("storage: list location parent edges: %w", err)
	}

	children := make(map[string][]string, len(edges))
	live := make(map[string]bool, len(edges))
	for _, e := range edges {
		live[e.ID] = !e.DeletedAt.Valid
		if e.ParentID.Valid {
			children[e.ParentID.String] = append(children[e.ParentID.String], e.ID)
		}
	}

	out := make([]string, 0, 8)
	visited := make(map[string]struct{}, len(edges))
	queue := []string{locationID}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if _, seen := visited[current]; seen {
			continue
		}
		visited[current] = struct{}{}
		if live[current] {
			out = append(out, current)
		}
		queue = append(queue, children[current]...)
	}
	return out, nil
}
