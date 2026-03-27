SET NAMES utf8mb4;

TRUNCATE TABLE `users`;
ALTER TABLE `users` AUTO_INCREMENT = 1;
INSERT INTO `users` (`id`, `name`, `display_name`, `description`, `passhash`) VALUES
    (1, 'admin', '管理者', 'admin です', 'cb6772e22a26944c7565399e69e765da4d74c9bc0d5559b587eb2bab63a5ccbd');

TRUNCATE TABLE `teams`;
ALTER TABLE `teams` AUTO_INCREMENT = 1;

TRUNCATE TABLE `tasks`;
ALTER TABLE `tasks` AUTO_INCREMENT = 1;

TRUNCATE TABLE `subtasks`;
ALTER TABLE `subtasks` AUTO_INCREMENT = 1;

TRUNCATE TABLE `answers`;
ALTER TABLE `answers` AUTO_INCREMENT = 1;

TRUNCATE TABLE `submissions`;
ALTER TABLE `submissions` AUTO_INCREMENT = 1;
