TRUNCATE TABLE `users`;
ALTER TABLE `users` AUTO_INCREMENT = 1;
INSERT INTO `users` (`id`, `name`, `display_name`, `description`, `passhash`) VALUES
    (1, 'admin', '管理者', 'admin です', '8c6976e5b5410415bde908bd4dee15dfb167a9c873fc4bb8a81f6f2ab448a918'),
    (2, 'risucon1', 'risucon1', 'テスト用ユーザー1', '0778c5cb31e8c87408ced3bf15467f371c9da0810b856a18ccb1ba00cda7b4d2'),
    (3, 'risucon2', 'risucon2', 'テスト用ユーザー2', 'c9fbc16ab8be26f057448c2eaaf9c5105b737374b06becd2c6817cf978f97020'),
    (4, 'risucon3', 'risucon3', 'テスト用ユーザー3', '0a414dbc7b463040ab92cb820bb89884b1dbe3ae974d5ed5ebf4246b79d0f0fb'),
    (5, 'risucon4', 'risucon4', 'テスト用ユーザー4', '9a2f790ffe815adebec3f30a849b26a7262c81abe72ba683064c66bd331c976b');

TRUNCATE TABLE `teams`;
ALTER TABLE `teams` AUTO_INCREMENT = 1;
INSERT INTO `teams` (`id`, `name`, `display_name`, `leader_id`, `member1_id`, `member2_id`, `description`, `invitation_code`) VALUES
    (1, 'team1', 'チーム1', 2, 3, 4, 'チーム1 です', '5e52b7b57ca5dc6d'),
    (2, 'team2', 'チーム2', 5, -1, -1, 'チーム2 です', 'd4dd78d422d8a980');

TRUNCATE TABLE `tasks`;
ALTER TABLE `tasks` AUTO_INCREMENT = 1;
INSERT INTO `tasks` (`id`, `name`, `display_name`, `statement`, `submission_limit`) VALUES
    (1, 'A', '足し算', '足し算をしてください。', 15),
    (2, 'B', '引き算', '引き算をしてください。', 20);

TRUNCATE TABLE `subtasks`;
ALTER TABLE `subtasks` AUTO_INCREMENT = 1;
INSERT INTO `subtasks` (`id`, `name`, `display_name`, `task_id`, `statement`) VALUES
    (1, 'A_1', '(1)', 1, '1+1=?'),
    (2, 'A_2', '(2)', 1, '1+2=?'),
    (3, 'B_1', '(1)', 2, '1-1=?'),
    (4, 'B_2', '(2)', 2, '1-2=? (符号のみが間違っていた場合、部分点が与えられる。)');

TRUNCATE TABLE `answers`;
ALTER TABLE `answers` AUTO_INCREMENT = 1;
INSERT INTO `answers` (`id`, `task_id`, `subtask_id`, `answer`, `score`) VALUES
    (1, 1, 1, '2', 10),
    (2, 1, 2, '3', 10),
    (3, 2, 3, '0', 10),
    (4, 2, 4, '-1', 10),
    (5, 2, 4, '1', 5);

TRUNCATE TABLE `submissions`;
ALTER TABLE `submissions` AUTO_INCREMENT = 1;
INSERT INTO `submissions` (`id`, `task_id`, `user_id`, `submitted_at`, `answer`) VALUES
    (1, 1, 2, '2012-06-20 00:00:00', '2'),
    (2, 1, 3, '2012-06-20 00:00:01', '3'),
    (3, 2, 4, '2012-06-20 00:00:02', '0'),
    (4, 2, 2, '2012-06-20 00:00:03', '-1'),
    (5, 2, 3, '2012-06-20 00:00:04', '1'),
    (6, 2, 5, '2012-06-20 00:00:04', '-1');