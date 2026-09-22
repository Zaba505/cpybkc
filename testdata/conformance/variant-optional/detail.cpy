      *****************************************************************
      * The record this entry is about: a table that is there or is
      * not, and whose one entry chooses its own alternative.
      *
      * An unsigned zoned count, a group occurring zero or one times
      * depending on it, a REDEFINES inside that group, and a
      * four-byte item behind the table.
      *
      * It is variant-sliding's shape at the one declared maximum
      * where a consumer can mistake a table for an ordinary group,
      * and odo-optional's shape with an alternation inside it. Until
      * a consumer treats a maximum of one as a table, the REDEFINES
      * inside it is not inside a table either -- so it is two
      * descriptions of the record rather than one alternation chosen
      * per occurrence, and a reader built that way has no variant to
      * take an arm of.
      *
      * Each entry carries a kind byte and a body:
      *
      *   VOP-KIND       X(1)    offset 0 of the entry, 1 byte
      *   VOP-NOTE-BODY          offset 1, 6 bytes
      *     VOP-HEAD     X(2)    offset 1, 2 bytes
      *     VOP-TEXT     X(4)    offset 3, 4 bytes
      *   VOP-NOTE-CODE          offset 1, 6 bytes, redefining it
      *     VOP-REF      X(4)    offset 1, 4 bytes
      *     ..slack..            offset 5, 2 bytes
      *
      * VOP-NOTE-CODE is two bytes shorter than the run it redefines,
      * which is ordinary COBOL: the entry is six bytes wide whichever
      * alternative it carries, and in a coded entry two of them
      * belong to no item at all. Those two travel with the occurrence
      * they were read from and are put back where they were found.
      *
      * VOP-TAIL is what turns a presence error into a visible one.
      * Under the sliding reading it begins at the byte after the last
      * occurrence the count states, so on a record carrying no
      * occurrence it begins immediately after the count -- and a
      * consumer that read the group anyway lands seven bytes late and
      * runs off the end of a five-byte record.
      *
      * The extent is therefore 5 + 7n bytes rather than a constant,
      * which is why the file this record sits in is framed.
      *****************************************************************
       01  VOP-DETAIL.
           05  VOP-NOTE-COUNT          PIC 9(1).
           05  VOP-NOTE OCCURS 0 TO 1 TIMES
                       DEPENDING ON VOP-NOTE-COUNT.
               10  VOP-KIND            PIC X(1).
               10  VOP-NOTE-BODY.
                   15  VOP-HEAD        PIC X(2).
                   15  VOP-TEXT        PIC X(4).
               10  VOP-NOTE-CODE REDEFINES VOP-NOTE-BODY.
                   15  VOP-REF         PIC X(4).
           05  VOP-TAIL                PIC X(4).
