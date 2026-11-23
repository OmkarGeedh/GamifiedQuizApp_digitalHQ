package repo

import "context"

// DeleteClientAccount deletes a player. Every table that references clients
// cascades (ledger, games, history, rewards, purchases, subscriptions, ads,
// analytics); billing_audit keeps its rows with client_id set to NULL, so the
// fraud trail survives without personal data. Reports whether a row was deleted.
func DeleteClientAccount(ctx context.Context, clientID int) (bool, error) {
	res := GetDB().WithContext(ctx).Exec(`DELETE FROM clients WHERE id = ?`, clientID)
	return res.RowsAffected > 0, res.Error
}
