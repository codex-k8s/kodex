WITH organization AS (
    INSERT INTO control_plane.organizations(ref, name)
    VALUES ('org_assistant_core_foreign', 'Foreign assistant core fixture')
    RETURNING id
), actor AS (
    INSERT INTO control_plane.subjects(organization_id, ref, issuer, external_subject_digest, display_name)
    SELECT id, 'usr_assistant_core_foreign', 'assistant-core-fixture', repeat('f', 64), 'Foreign fixture actor'
    FROM organization
    RETURNING id, organization_id
)
INSERT INTO control_plane.projects(ref, organization_id, name, created_by)
SELECT 'prj_assistant_core_foreign', organization_id, 'Foreign assistant core project', id
FROM actor
RETURNING ref;
