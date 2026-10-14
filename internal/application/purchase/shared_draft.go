package purchase

import (
	"context"
	"database/sql"
	stdjson "encoding/json"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/application/txcleanup"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

const maxSharedPurchaseDraftPayloadBytes = 256 * 1024

type SharedPurchaseDraft struct {
	ID          string
	HouseholdID string
	Revision    int64
	Payload     string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type UpdateSharedPurchaseDraftResult struct {
	Draft    SharedPurchaseDraft
	Conflict bool
}

func (s *Service) CreateSharedPurchaseDraft(ctx context.Context, actorID, householdID, payload string) (SharedPurchaseDraft, error) {
	if len(payload) > maxSharedPurchaseDraftPayloadBytes {
		return SharedPurchaseDraft{}, fmt.Errorf("%w: shared purchase draft payload exceeds size limit", ErrBadInput)
	}
	householdUUID, err := parseUUID(householdID, "household")
	if err != nil {
		return SharedPurchaseDraft{}, err
	}
	actorUUID, err := parseUUID(actorID, "actor")
	if err != nil {
		return SharedPurchaseDraft{}, err
	}
	refs, encoded, err := validateSharedPurchaseDraftPayload(payload)
	if err != nil {
		return SharedPurchaseDraft{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return SharedPurchaseDraft{}, err
	}
	committed := false
	defer func() {
		if !committed {
			txcleanup.Rollback(ctx, tx)
		}
	}()
	executor := bobpgx.NewTx(tx, nil)
	if err := validateSharedDraftAccess(ctx, executor, householdUUID, actorUUID); err != nil {
		return SharedPurchaseDraft{}, err
	}
	if err := validateSharedDraftReferences(ctx, executor, householdUUID, refs); err != nil {
		return SharedPurchaseDraft{}, err
	}
	id, err := gofrsuuid.NewV7()
	if err != nil {
		return SharedPurchaseDraft{}, fmt.Errorf("generate shared purchase draft id: %w", err)
	}
	revision := int64(1)
	jsonPayload := types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(encoded)}
	row, err := models.SharedPurchaseDrafts.Insert(&models.SharedPurchaseDraftSetter{
		ID: &id, HouseholdID: &householdUUID, Revision: &revision, Payload: &jsonPayload,
	}).One(ctx, executor)
	if err != nil {
		return SharedPurchaseDraft{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SharedPurchaseDraft{}, err
	}
	committed = true
	return sharedPurchaseDraftFromRow(row), nil
}

func (s *Service) GetSharedPurchaseDraft(ctx context.Context, actorID, draftID string) (SharedPurchaseDraft, error) {
	actorUUID, err := parseUUID(actorID, "actor")
	if err != nil {
		return SharedPurchaseDraft{}, err
	}
	draftUUID, err := parseUUID(draftID, "shared purchase draft")
	if err != nil {
		return SharedPurchaseDraft{}, err
	}
	executor := bobpgx.NewPool(s.db)
	row, err := models.SharedPurchaseDrafts.Query(sm.Where(models.SharedPurchaseDrafts.Columns.ID.EQ(psql.Arg(draftUUID)))).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return SharedPurchaseDraft{}, ErrNotFound
	}
	if err != nil {
		return SharedPurchaseDraft{}, err
	}
	if err := validateSharedDraftMembership(ctx, executor, row.HouseholdID, actorUUID); err != nil {
		return SharedPurchaseDraft{}, err
	}
	return sharedPurchaseDraftFromRow(row), nil
}

func (s *Service) UpdateSharedPurchaseDraft(ctx context.Context, actorID, draftID string, expectedRevision int64, payload string) (UpdateSharedPurchaseDraftResult, error) {
	actorUUID, err := parseUUID(actorID, "actor")
	if err != nil {
		return UpdateSharedPurchaseDraftResult{}, err
	}
	draftUUID, err := parseUUID(draftID, "shared purchase draft")
	if err != nil {
		return UpdateSharedPurchaseDraftResult{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return UpdateSharedPurchaseDraftResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			txcleanup.Rollback(ctx, tx)
		}
	}()
	executor := bobpgx.NewTx(tx, nil)
	row, err := models.SharedPurchaseDrafts.Query(sm.Where(models.SharedPurchaseDrafts.Columns.ID.EQ(psql.Arg(draftUUID))), sm.ForUpdate()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		return UpdateSharedPurchaseDraftResult{}, ErrNotFound
	}
	if err != nil {
		return UpdateSharedPurchaseDraftResult{}, err
	}
	if err := validateSharedDraftMembership(ctx, executor, row.HouseholdID, actorUUID); err != nil {
		return UpdateSharedPurchaseDraftResult{}, err
	}
	if row.Revision != expectedRevision {
		if err := tx.Commit(ctx); err != nil {
			return UpdateSharedPurchaseDraftResult{}, err
		}
		committed = true
		return UpdateSharedPurchaseDraftResult{Draft: sharedPurchaseDraftFromRow(row), Conflict: true}, nil
	}
	refs, encoded, err := validateSharedPurchaseDraftPayload(payload)
	if err != nil {
		return UpdateSharedPurchaseDraftResult{}, err
	}
	if err := validateSharedDraftReferences(ctx, executor, row.HouseholdID, refs); err != nil {
		return UpdateSharedPurchaseDraftResult{}, err
	}
	revision := row.Revision + 1
	updatedAt := time.Now().UTC()
	jsonPayload := types.JSON[stdjson.RawMessage]{Val: stdjson.RawMessage(encoded)}
	if _, err := models.SharedPurchaseDrafts.Update((&models.SharedPurchaseDraftSetter{
		Revision: &revision, Payload: &jsonPayload, UpdatedAt: &updatedAt,
	}).UpdateMod(), um.Where(psql.And(
		models.SharedPurchaseDrafts.Columns.ID.EQ(psql.Arg(draftUUID)),
		models.SharedPurchaseDrafts.Columns.Revision.EQ(psql.Arg(expectedRevision)),
	))).Exec(ctx, executor); err != nil {
		return UpdateSharedPurchaseDraftResult{}, err
	}
	updated, err := models.SharedPurchaseDrafts.Query(sm.Where(models.SharedPurchaseDrafts.Columns.ID.EQ(psql.Arg(draftUUID)))).One(ctx, executor)
	if err != nil {
		return UpdateSharedPurchaseDraftResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return UpdateSharedPurchaseDraftResult{}, err
	}
	committed = true
	return UpdateSharedPurchaseDraftResult{Draft: sharedPurchaseDraftFromRow(updated)}, nil
}

func sharedPurchaseDraftFromRow(row *models.SharedPurchaseDraft) SharedPurchaseDraft {
	return SharedPurchaseDraft{
		ID: row.ID.String(), HouseholdID: row.HouseholdID.String(), Revision: row.Revision,
		Payload: string(row.Payload.Val), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

type sharedPurchaseDraftReferences struct {
	accounts   []gofrsuuid.UUID
	categories []gofrsuuid.UUID
	pools      []gofrsuuid.UUID
	variants   []gofrsuuid.UUID
}

func validateSharedDraftAccess(ctx context.Context, executor bob.Executor, householdID, actorID gofrsuuid.UUID) error {
	exists, err := models.Households.Query(sm.Where(models.Households.Columns.ID.EQ(psql.Arg(householdID)))).Exists(ctx, executor)
	if err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return validateSharedDraftMembership(ctx, executor, householdID, actorID)
}

func validateSharedDraftMembership(ctx context.Context, executor bob.Executor, householdID, actorID gofrsuuid.UUID) error {
	_, err := models.HouseholdMembers.Query(sm.Where(psql.And(
		models.HouseholdMembers.Columns.HouseholdID.EQ(psql.Arg(householdID)),
		models.HouseholdMembers.Columns.MemberID.EQ(psql.Arg(actorID)),
	)), sm.ForShare()).One(ctx, executor)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

func validateSharedDraftReferences(ctx context.Context, executor bob.Executor, householdID gofrsuuid.UUID, refs sharedPurchaseDraftReferences) error {
	if len(refs.accounts) > 0 {
		rows, err := models.Accounts.Query(sm.Where(psql.And(
			models.Accounts.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.Accounts.Columns.ID.In(jsonUUIDArgs(refs.accounts)...),
		))).All(ctx, executor)
		if err != nil {
			return err
		}
		if len(rows) != len(refs.accounts) {
			return fmt.Errorf("%w: account reference does not belong to household", ErrBadInput)
		}
	}
	if len(refs.categories) > 0 {
		rows, err := models.Categories.Query(sm.Where(psql.And(
			models.Categories.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.Categories.Columns.ID.In(jsonUUIDArgs(refs.categories)...),
		))).All(ctx, executor)
		if err != nil {
			return err
		}
		if len(rows) != len(refs.categories) {
			return fmt.Errorf("%w: category reference does not belong to household", ErrBadInput)
		}
	}
	if len(refs.pools) > 0 {
		rows, err := models.Pools.Query(sm.Where(psql.And(
			models.Pools.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.Pools.Columns.ID.In(jsonUUIDArgs(refs.pools)...),
		))).All(ctx, executor)
		if err != nil {
			return err
		}
		if len(rows) != len(refs.pools) {
			return fmt.Errorf("%w: pool reference does not belong to household", ErrBadInput)
		}
	}
	if len(refs.variants) > 0 {
		rows, err := models.ProductVariants.Query(sm.Where(psql.And(
			models.ProductVariants.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.ProductVariants.Columns.ID.In(jsonUUIDArgs(refs.variants)...),
		))).All(ctx, executor)
		if err != nil {
			return err
		}
		if len(rows) != len(refs.variants) {
			return fmt.Errorf("%w: product variant reference does not belong to household", ErrBadInput)
		}
	}
	return nil
}

func validateSharedPurchaseDraftPayload(payload string) (sharedPurchaseDraftReferences, jsontext.Value, error) {
	invalid := sharedPurchaseDraftReferences{}
	if len(payload) == 0 || len(payload) > maxSharedPurchaseDraftPayloadBytes {
		return invalid, nil, fmt.Errorf("%w: shared purchase draft payload has invalid size", ErrBadInput)
	}
	var root map[string]jsontext.Value
	if err := json.Unmarshal([]byte(payload), &root); err != nil || root == nil {
		return invalid, nil, fmt.Errorf("%w: shared purchase draft payload must be a JSON object", ErrBadInput)
	}
	if err := validateJSONKeys(root, "merchant", "occurredAt", "paymentAccountId", "paymentAllocations", "paymentAutoTotal", "paymentAmountManuallyEdited", "itemName", "itemAmount", "selectedProductVariantId", "categoryId", "items"); err != nil {
		return invalid, nil, err
	}
	for _, field := range []string{"merchant", "occurredAt", "itemName", "itemAmount", "categoryId"} {
		if err := requireJSONString(root, field); err != nil {
			return invalid, nil, err
		}
	}
	refs := sharedPurchaseDraftReferences{}
	for _, field := range []string{"paymentAccountId", "selectedProductVariantId"} {
		id, present, err := optionalJSONString(root, field)
		if err != nil {
			return invalid, nil, err
		}
		if present && id != "" {
			parsed, err := parseUUID(id, field)
			if err != nil {
				return invalid, nil, err
			}
			if field == "paymentAccountId" {
				refs.accounts = append(refs.accounts, parsed)
			} else {
				refs.variants = append(refs.variants, parsed)
			}
		}
	}
	categoryID, err := requiredJSONString(root, "categoryId")
	if err != nil {
		return invalid, nil, err
	}
	if categoryID != "" {
		parsed, err := parseUUID(categoryID, "category")
		if err != nil {
			return invalid, nil, err
		}
		refs.categories = append(refs.categories, parsed)
	}
	for _, field := range []string{"paymentAutoTotal", "paymentAmountManuallyEdited"} {
		if err := optionalJSONBool(root, field); err != nil {
			return invalid, nil, err
		}
	}
	if raw, ok := root["paymentAllocations"]; ok {
		var allocations []jsontext.Value
		if err := json.Unmarshal(raw, &allocations); err != nil || allocations == nil {
			return invalid, nil, fmt.Errorf("%w: paymentAllocations must be an array", ErrBadInput)
		}
		for _, rawAllocation := range allocations {
			var allocation map[string]jsontext.Value
			if err := json.Unmarshal(rawAllocation, &allocation); err != nil || allocation == nil {
				return invalid, nil, fmt.Errorf("%w: payment allocation must be an object", ErrBadInput)
			}
			if err := validateJSONKeys(allocation, "paymentAccountId", "amountYen"); err != nil {
				return invalid, nil, err
			}
			accountID, err := requiredJSONString(allocation, "paymentAccountId")
			if err != nil {
				return invalid, nil, err
			}
			if accountID != "" {
				parsed, err := parseUUID(accountID, "payment account")
				if err != nil {
					return invalid, nil, err
				}
				refs.accounts = append(refs.accounts, parsed)
			}
			if err := requireJSONString(allocation, "amountYen"); err != nil {
				return invalid, nil, err
			}
		}
	}
	itemsRaw, ok := root["items"]
	if !ok {
		return invalid, nil, fmt.Errorf("%w: items is required", ErrBadInput)
	}
	var items []jsontext.Value
	if err := json.Unmarshal(itemsRaw, &items); err != nil || items == nil {
		return invalid, nil, fmt.Errorf("%w: items must be an array", ErrBadInput)
	}
	for _, rawItem := range items {
		var item map[string]jsontext.Value
		if err := json.Unmarshal(rawItem, &item); err != nil || item == nil {
			return invalid, nil, fmt.Errorf("%w: purchase draft item must be an object", ErrBadInput)
		}
		if err := validateJSONKeys(item, "key", "name", "amount", "quantity", "quantityUnit", "categoryId", "productVariantId", "allocations"); err != nil {
			return invalid, nil, err
		}
		for _, field := range []string{"key", "name", "amount", "categoryId"} {
			if err := requireJSONString(item, field); err != nil {
				return invalid, nil, err
			}
		}
		for _, field := range []string{"quantity", "quantityUnit", "productVariantId"} {
			if _, _, err := optionalJSONString(item, field); err != nil {
				return invalid, nil, err
			}
		}
		itemCategoryID, err := requiredJSONString(item, "categoryId")
		if err != nil {
			return invalid, nil, err
		}
		if itemCategoryID != "" {
			parsed, err := parseUUID(itemCategoryID, "category")
			if err != nil {
				return invalid, nil, err
			}
			refs.categories = append(refs.categories, parsed)
		}
		variantID, _, err := optionalJSONString(item, "productVariantId")
		if err != nil {
			return invalid, nil, err
		}
		if variantID != "" {
			parsed, err := parseUUID(variantID, "product variant")
			if err != nil {
				return invalid, nil, err
			}
			refs.variants = append(refs.variants, parsed)
		}
		allocationsRaw, ok := item["allocations"]
		if !ok {
			return invalid, nil, fmt.Errorf("%w: allocations is required", ErrBadInput)
		}
		var allocations []jsontext.Value
		if err := json.Unmarshal(allocationsRaw, &allocations); err != nil || allocations == nil {
			return invalid, nil, fmt.Errorf("%w: allocations must be an array", ErrBadInput)
		}
		for _, rawAllocation := range allocations {
			var allocation map[string]jsontext.Value
			if err := json.Unmarshal(rawAllocation, &allocation); err != nil || allocation == nil {
				return invalid, nil, fmt.Errorf("%w: allocation must be an object", ErrBadInput)
			}
			if err := validateJSONKeys(allocation, "poolId", "weight"); err != nil {
				return invalid, nil, err
			}
			poolID, err := requiredJSONString(allocation, "poolId")
			if err != nil {
				return invalid, nil, err
			}
			if poolID != "" {
				parsed, err := parseUUID(poolID, "pool")
				if err != nil {
					return invalid, nil, err
				}
				refs.pools = append(refs.pools, parsed)
			}
			weightRaw, ok := allocation["weight"]
			var weight float64
			if !ok || !isJSONNumber(weightRaw) || json.Unmarshal(weightRaw, &weight) != nil || math.IsNaN(weight) || math.IsInf(weight, 0) {
				return invalid, nil, fmt.Errorf("%w: allocation weight must be a finite number", ErrBadInput)
			}
		}
	}
	refs.accounts = uniqueSharedDraftIDs(refs.accounts)
	refs.categories = uniqueSharedDraftIDs(refs.categories)
	refs.pools = uniqueSharedDraftIDs(refs.pools)
	refs.variants = uniqueSharedDraftIDs(refs.variants)
	encoded, err := json.Marshal(root)
	if err != nil {
		return invalid, nil, fmt.Errorf("%w: could not encode shared purchase draft payload", ErrBadInput)
	}
	return refs, encoded, nil
}

func validateJSONKeys(value map[string]jsontext.Value, allowed ...string) error {
	for key := range value {
		if !slices.Contains(allowed, key) {
			return fmt.Errorf("%w: unsupported purchase draft field", ErrBadInput)
		}
	}
	return nil
}

func requireJSONString(value map[string]jsontext.Value, key string) error {
	_, err := requiredJSONString(value, key)
	return err
}

func requiredJSONString(value map[string]jsontext.Value, key string) (string, error) {
	raw, ok := value[key]
	if !ok {
		return "", fmt.Errorf("%w: %s is required", ErrBadInput, key)
	}
	if !isJSONString(raw) {
		return "", fmt.Errorf("%w: %s must be a string", ErrBadInput, key)
	}
	var decoded string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", fmt.Errorf("%w: %s must be a string", ErrBadInput, key)
	}
	return decoded, nil
}

func optionalJSONString(value map[string]jsontext.Value, key string) (string, bool, error) {
	raw, ok := value[key]
	if !ok {
		return "", false, nil
	}
	if !isJSONString(raw) {
		return "", true, fmt.Errorf("%w: %s must be a string", ErrBadInput, key)
	}
	var decoded string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", true, fmt.Errorf("%w: %s must be a string", ErrBadInput, key)
	}
	return decoded, true, nil
}

func optionalJSONBool(value map[string]jsontext.Value, key string) error {
	raw, ok := value[key]
	if !ok {
		return nil
	}
	text := strings.TrimSpace(string(raw))
	if text != "true" && text != "false" {
		return fmt.Errorf("%w: %s must be a boolean", ErrBadInput, key)
	}
	var decoded bool
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return fmt.Errorf("%w: %s must be a boolean", ErrBadInput, key)
	}
	return nil
}

func isJSONString(value jsontext.Value) bool {
	value = jsontext.Value(strings.TrimSpace(string(value)))
	return len(value) > 0 && value[0] == '"'
}

func isJSONNumber(value jsontext.Value) bool {
	value = jsontext.Value(strings.TrimSpace(string(value)))
	return len(value) > 0 && strings.ContainsRune("-0123456789", rune(value[0]))
}

func jsonUUIDArgs(ids []gofrsuuid.UUID) []bob.Expression {
	args := make([]bob.Expression, len(ids))
	for i, id := range ids {
		args[i] = psql.Arg(id)
	}
	return args
}

func uniqueSharedDraftIDs(ids []gofrsuuid.UUID) []gofrsuuid.UUID {
	seen := make(map[gofrsuuid.UUID]struct{}, len(ids))
	unique := make([]gofrsuuid.UUID, 0, len(ids))
	for _, id := range ids {
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	return unique
}
