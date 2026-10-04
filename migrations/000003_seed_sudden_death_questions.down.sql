-- Migration: 000003_seed_sudden_death_questions.down.sql
-- Description: Remove the 10 Sudden Death questions seeded in 000003

DELETE FROM questions WHERE question_code IN (
    'sd_acc_001', 'sd_acc_002', 'sd_acc_003', 'sd_acc_004', 'sd_acc_005',
    'sd_acc_006', 'sd_acc_007', 'sd_acc_008', 'sd_acc_009', 'sd_acc_010'
);
