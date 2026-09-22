      *****************************************************************
      * The record this entry is about: a group that is there or is
      * not.
      *
      * An unsigned zoned count, a group occurring zero or one times
      * depending on it, and a four-byte item behind that group.
      *
      * The declared maximum is one, which is what separates this entry
      * from odo-sliding. Every other table in this corpus can be read
      * at the wrong length; this one can be read at the wrong
      * *presence*, and a consumer that treats a maximum of one as "not
      * a table" reads OPT-TEXT out of bytes belonging to OPT-TAIL on
      * every record whose count is zero.
      *
      * OPT-TAIL is what turns that into a visible error rather than a
      * silent one. Under the sliding reading it begins at the byte
      * after the last occurrence the count states, so on a record
      * carrying no occurrence it begins immediately after the count --
      * and a consumer that read the group anyway lands four bytes late
      * and runs off the end of the record.
      *
      * The extent is therefore 5 + 4n bytes rather than a constant,
      * which is why the file this record sits in is framed.
      *****************************************************************
       01  OPT-DETAIL.
           05  OPT-NOTE-COUNT          PIC 9(1).
           05  OPT-NOTE OCCURS 0 TO 1 TIMES
                       DEPENDING ON OPT-NOTE-COUNT.
               10  OPT-TEXT            PIC X(4).
           05  OPT-TAIL                PIC X(4).
