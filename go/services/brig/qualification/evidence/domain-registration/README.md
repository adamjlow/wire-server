# Domain registration read and guard checkpoint

Source `236274537014cfda7f3a4da727d77f10208735a9`. Full Go1.26.9 checks and static units in the unchanged native runner pass. Candidate `9219f798fd8f0cccba29269830dd76d5eb88f43a922deacb114d4c4c0f964e5c`; tagged store `bee33860d4cca831b5b777df7d94bece9af00983623d9a876488cce0c9382fbc`.

Actual native utility/schema checks pass all three storage-mode domain reads, canonical keys, SSO guard, ignored inconsistent combinations, rejected unknown tags, absent legacy tags and migration Cassandra-source/PG-only controls. Existing store checks remain green. Native utility0/test0/controller0/cleanup empty; independent observation resources empty.

This is read/guard proof, not domain management or activation/registration HTTP parity. Domain writes/team listing/migration write locks, exact historical URI-parser edges, activation/search/events/journal/team orchestration, registration and all remaining whole-service gates remain required. No protected oracle, Haskell, schema, dependency or policy change. Galley starts only after whole Brig completion.
