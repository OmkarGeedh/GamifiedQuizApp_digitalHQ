package repo

import (
	"context"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
)

func GetClientByEmail(ctx context.Context, email string) (*models.Client, error) {
	var client models.Client
	err := GetDB().WithContext(ctx).
		Where("email = ? AND status != ?", email, "deleted").
		First(&client).Error
	if err != nil {
		return nil, err
	}
	return &client, nil
}

func GetClientByID(ctx context.Context, id int) (*models.Client, error) {
	var client models.Client
	err := GetDB().WithContext(ctx).
		Where("id = ? AND status != ?", id, "deleted").
		First(&client).Error
	if err != nil {
		return nil, err
	}
	return &client, nil
}

func CreateClient(ctx context.Context, client *models.Client) error {
	return GetDB().WithContext(ctx).Create(client).Error
}

func UpdateClientRefreshToken(ctx context.Context, id int, refreshToken *string) error {
	return GetDB().WithContext(ctx).
		Model(&models.Client{}).
		Where("id = ?", id).
		Update("refresh_token", refreshToken).Error
}

func UpdateClientPassword(ctx context.Context, id int, hashedPassword string) error {
	return GetDB().WithContext(ctx).
		Model(&models.Client{}).
		Where("id = ?", id).
		Update("password", hashedPassword).Error
}

func CheckEmailExists(ctx context.Context, email string) (bool, error) {
	var count int64
	err := GetDB().WithContext(ctx).
		Model(&models.Client{}).
		Where("email = ? AND status != ?", email, "deleted").
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func CheckUsernameExists(ctx context.Context, username string) (bool, error) {
	var count int64
	err := GetDB().WithContext(ctx).
		Model(&models.Client{}).
		Where("username = ? AND status != ?", username, "deleted").
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func CheckPhoneExists(ctx context.Context, phone string) (bool, error) {
	var count int64
	err := GetDB().WithContext(ctx).
		Model(&models.Client{}).
		Where("phone = ? AND status != ?", phone, "deleted").
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func UpdateClientPhone(ctx context.Context, id int, phone *string) error {
	return GetDB().WithContext(ctx).
		Model(&models.Client{}).
		Where("id = ?", id).
		Update("phone", phone).Error
}
