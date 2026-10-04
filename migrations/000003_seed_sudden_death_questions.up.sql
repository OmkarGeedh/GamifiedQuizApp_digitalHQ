-- Migration: 000003_seed_sudden_death_questions.up.sql
-- Description: Seed the 10 2-option accounting questions for Sudden Death mode

CREATE TABLE IF NOT EXISTS questions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    question_code VARCHAR(20) NOT NULL,
    question_type VARCHAR(32) NOT NULL DEFAULT 'mcq',
    topic_id VARCHAR(64) NOT NULL,
    prompt TEXT NOT NULL,
    option_a TEXT NOT NULL,
    option_b TEXT NOT NULL,
    option_c TEXT NOT NULL DEFAULT '',
    option_d TEXT NOT NULL DEFAULT '',
    correct_option CHAR(1) NOT NULL,
    difficulty SMALLINT NOT NULL DEFAULT 1,
    points INT NOT NULL DEFAULT 10,
    hint TEXT,
    explanation TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_questions_question_code ON questions (question_code);
CREATE INDEX IF NOT EXISTS idx_questions_type ON questions (question_type);
CREATE INDEX IF NOT EXISTS idx_questions_topic ON questions (topic_id);

INSERT INTO questions (
    question_code, question_type, topic_id, prompt,
    option_a, option_b, option_c, option_d,
    correct_option, difficulty, points, hint, explanation
) VALUES
(
    'sd_acc_001', 'sudden_death', 'accounting',
    'Which account is debited when goods are purchased for cash?',
    'Purchases Account', 'Cash Account', '', '',
    'a', 1, 10,
    'Think about which account records goods bought for resale.',
    'Purchases Account is debited when goods are purchased for cash.'
),
(
    'sd_acc_002', 'sudden_death', 'accounting',
    'What type of account is Cash Account?',
    'Real Account', 'Personal Account', '', '',
    'a', 1, 10,
    'Tangible business assets are classified under this rule.',
    'Cash Account is a Real Account since it represents a tangible asset of the business.'
),
(
    'sd_acc_003', 'sudden_death', 'accounting',
    'Which golden rule states ''Debit the receiver, Credit the giver''?',
    'Real Account', 'Personal Account', '', '',
    'b', 1, 10,
    'This rule applies to persons, firms, and institutions.',
    'The golden rule ''Debit the receiver, Credit the giver'' applies to Personal Accounts.'
),
(
    'sd_acc_004', 'sudden_death', 'accounting',
    'What is the primary book of original entry called?',
    'Journal', 'Ledger', '', '',
    'a', 1, 10,
    'Transactions are recorded here chronologically first.',
    'The Journal is the primary book of original entry where financial transactions are first recorded in chronological order.'
),
(
    'sd_acc_005', 'sudden_death', 'accounting',
    'When rent is paid in cash, which account is credited?',
    'Rent Account', 'Cash Account', '', '',
    'b', 2, 10,
    'Credit what goes out of the business.',
    'When rent is paid in cash, cash goes out of the business, so Cash Account is credited.'
),
(
    'sd_acc_006', 'sudden_death', 'accounting',
    'Depreciation charged on machinery is debited to which account?',
    'Depreciation Account', 'Machinery Account', '', '',
    'a', 2, 10,
    'Debit all expenses and losses under the nominal rule.',
    'Depreciation is an expense, so Depreciation Account is debited under the nominal rule.'
),
(
    'sd_acc_007', 'sudden_death', 'accounting',
    'Which statement verifies the arithmetical accuracy of ledger accounts?',
    'Balance Sheet', 'Trial Balance', '', '',
    'b', 2, 10,
    'It lists debit and credit totals before final accounts.',
    'A Trial Balance is prepared to check and verify the arithmetical accuracy of ledger account balances.'
),
(
    'sd_acc_008', 'sudden_death', 'accounting',
    'Goods returned by a customer are recorded in which book?',
    'Sales Returns Book', 'Purchases Returns Book', '', '',
    'a', 2, 10,
    'Also known as the return inwards book.',
    'Goods returned by customers are recorded in the Sales Returns Book (Return Inwards Book).'
),
(
    'sd_acc_009', 'sudden_death', 'accounting',
    'An error of omission occurs when a transaction is:',
    'Recorded in the wrong subsidiary book', 'Completely or partially not recorded', '', '',
    'b', 3, 10,
    'The transaction was forgotten or left out entirely.',
    'An error of omission happens when a transaction is completely or partially omitted from entry in the accounting books.'
),
(
    'sd_acc_010', 'sudden_death', 'accounting',
    'According to the Dual Aspect concept, Total Assets always equal:',
    'Total Liabilities plus Capital', 'Capital minus Liabilities', '', '',
    'a', 3, 10,
    'This forms the fundamental accounting balance sheet equation.',
    'Under the Dual Aspect concept, Total Assets = Total Liabilities + Capital.'
)
ON CONFLICT (question_code) DO UPDATE SET
    question_type = EXCLUDED.question_type,
    topic_id = EXCLUDED.topic_id,
    prompt = EXCLUDED.prompt,
    option_a = EXCLUDED.option_a,
    option_b = EXCLUDED.option_b,
    option_c = EXCLUDED.option_c,
    option_d = EXCLUDED.option_d,
    correct_option = EXCLUDED.correct_option,
    difficulty = EXCLUDED.difficulty,
    points = EXCLUDED.points,
    hint = EXCLUDED.hint,
    explanation = EXCLUDED.explanation,
    updated_at = CURRENT_TIMESTAMP;
