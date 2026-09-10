ALTER TABLE tasks
 ADD COLUMN priority text NOT NULL DEFAULT 'none' CHECK (priority IN ('none','low','medium','high','urgent')),
 ADD COLUMN priority_version bigint NOT NULL DEFAULT 1,
 ADD COLUMN type text NOT NULL DEFAULT 'none' CHECK (type IN ('none','bug','chore','feature')),
 ADD COLUMN type_version bigint NOT NULL DEFAULT 1,
 ADD COLUMN size text NOT NULL DEFAULT 'none' CHECK (size IN ('none','xs','s','m','l','xl')),
 ADD COLUMN size_version bigint NOT NULL DEFAULT 1;
