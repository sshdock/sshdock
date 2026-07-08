create schema if not exists api;

create table if not exists api.messages (
  id serial primary key,
  body text not null
);

insert into api.messages (body)
values ('SSHDock API Postgres OK')
on conflict do nothing;

do $$
begin
  if not exists (select from pg_roles where rolname = 'web_anon') then
    create role web_anon nologin;
  end if;
end
$$;

grant usage on schema api to web_anon;
grant select on api.messages to web_anon;
