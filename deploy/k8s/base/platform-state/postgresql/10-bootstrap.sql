\set ON_ERROR_STOP on

CREATE ROLE control_plane_owner
    NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
CREATE ROLE control_plane_runtime
    NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
CREATE ROLE control_plane_migrator
    LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
CREATE ROLE control_plane_runtime_g1
    LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE INHERIT
    NOREPLICATION NOBYPASSRLS;
GRANT control_plane_owner TO control_plane_migrator
    WITH INHERIT FALSE, SET TRUE, ADMIN FALSE;
GRANT control_plane_runtime TO control_plane_runtime_g1
    WITH INHERIT TRUE, SET TRUE, ADMIN FALSE;
ALTER ROLE control_plane_migrator SET ROLE TO control_plane_owner;
CREATE DATABASE control_plane OWNER control_plane_owner;

CREATE ROLE internal_rpc_authority_owner
    NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
CREATE ROLE internal_rpc_authority_migrator
    LOGIN NOSUPERUSER NOCREATEDB CREATEROLE NOINHERIT
    NOREPLICATION NOBYPASSRLS;
GRANT internal_rpc_authority_owner TO internal_rpc_authority_migrator
    WITH INHERIT FALSE, SET TRUE, ADMIN TRUE;
ALTER ROLE internal_rpc_authority_migrator
    SET ROLE TO internal_rpc_authority_owner;
CREATE DATABASE internal_rpc_authority OWNER internal_rpc_authority_owner;

REVOKE CONNECT ON DATABASE control_plane FROM PUBLIC;
REVOKE CONNECT ON DATABASE internal_rpc_authority FROM PUBLIC;
GRANT CONNECT, TEMPORARY ON DATABASE control_plane
    TO control_plane_migrator, control_plane_runtime_g1;
GRANT CONNECT, CREATE, TEMPORARY ON DATABASE internal_rpc_authority
    TO internal_rpc_authority_migrator;
