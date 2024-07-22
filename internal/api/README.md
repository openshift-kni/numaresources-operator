# numaresources operator *INTERNAL* APIs

Mimicing kubernetes API, the master definition of the schema is
done using go structs, so we can have comments, annotations and so forth.

Unlike kubernetes API, we don't have (and unlikely we will ever have)
autogeneration of JSON documents, validation and so forth, because
this API is really simple ans semi-private and should be kept so.
