      *****************************************************************
      * The same two bytes under two PICTUREs.
      *
      * FF FF is 65535 in an item with no S and -1 in one with an S. The
      * difference is not in the bytes, so it is the PICTURE that carries it
      * and the accessor a generator chooses that acts on it.
      *****************************************************************
       01  COMP5-RECORD.
           05  C5-UNSIGNED             PIC 9(4) COMP-5.
           05  C5-SIGNED               PIC S9(4) COMP-5.
