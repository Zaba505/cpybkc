000010/
000020* The kitchen sink: every production of the pinned cobol-go       KITCHEN
000030* data-description grammar, in fixed reference format, for the    KITCHEN
000040* round trip in roundtrip_test.go. Columns 1-6 and 73-80 are the  KITCHEN
000050* sequence and identification areas, which cobol-go reads away.   KITCHEN
000060 01  KITCHEN-SINK GLOBAL EXTERNAL.                                KITCHEN
000070     05  LEVELS.                                                  KITCHEN
000080       10  LEVEL-10 PIC X.                                        KITCHEN
000090       07  LEVEL-07.                                              KITCHEN
000100         49  LEVEL-49 PIC X.                                      KITCHEN
000110         15  LEVEL-15 PIC X.                                      KITCHEN
000120     03  LEVEL-03.                                                KITCHEN
000130* FILLER, an omitted name, and names the S-expression grammar     KITCHEN
000140* reads otherwise.                                                KITCHEN
000150     05  FILLER PIC X.                                            KITCHEN
000160     05  filler PIC X.                                            KITCHEN
000170     05  PIC X(2).                                                KITCHEN
000180     05  lower-case-name PIC X.                                   KITCHEN
000190     05  nil PIC X VALUE nil.                                     KITCHEN
000200     05  NIL-CODE PIC X.                                          KITCHEN
000210       88  FILLER VALUE "F".                                      KITCHEN
000220       88  nil VALUE "N".                                         KITCHEN
000230* Every usage, written bare and after USAGE IS.                   KITCHEN
000240     05  USAGES.                                                  KITCHEN
000250       10  U-DISPLAY-BARE PIC 9(3) DISPLAY.                       KITCHEN
000260       10  U-DISPLAY-IS PIC 9(3) USAGE IS DISPLAY.                KITCHEN
000270       10  U-BINARY-BARE PIC 9(3) BINARY.                         KITCHEN
000280       10  U-BINARY-IS PIC 9(3) USAGE IS BINARY.                  KITCHEN
000290       10  U-PD-BARE PIC 9(3) PACKED-DECIMAL.                     KITCHEN
000300       10  U-PD-IS PIC 9(3) USAGE IS PACKED-DECIMAL.              KITCHEN
000310       10  U-COMP-BARE PIC 9(3) COMP.                             KITCHEN
000320       10  U-COMP-IS PIC 9(3) USAGE IS COMP.                      KITCHEN
000330       10  U-COMP1-BARE COMP-1.                                   KITCHEN
000340       10  U-COMP1-IS USAGE IS COMP-1.                            KITCHEN
000350       10  U-COMP2-BARE COMP-2.                                   KITCHEN
000360       10  U-COMP2-IS USAGE IS COMP-2.                            KITCHEN
000370       10  U-COMP3-BARE PIC 9(3) COMP-3.                          KITCHEN
000380       10  U-COMP3-IS PIC 9(3) USAGE IS COMP-3.                   KITCHEN
000390       10  U-COMP4-BARE PIC 9(3) COMP-4.                          KITCHEN
000400       10  U-COMP4-IS PIC 9(3) USAGE IS COMP-4.                   KITCHEN
000410       10  U-COMP5-BARE PIC 9(3) COMP-5.                          KITCHEN
000420       10  U-COMP5-IS PIC 9(3) USAGE IS COMP-5.                   KITCHEN
000430       10  U-COMP6-BARE PIC 9(3) COMP-6.                          KITCHEN
000440       10  U-COMP6-IS PIC 9(3) USAGE IS COMP-6.                   KITCHEN
000450       10  U-INDEX-BARE INDEX.                                    KITCHEN
000460       10  U-INDEX-IS USAGE IS INDEX.                             KITCHEN
000470       10  U-POINTER-BARE POINTER.                                KITCHEN
000480       10  U-POINTER-IS USAGE IS POINTER.                         KITCHEN
000490       10  U-LOWER PIC 9(3) comp-3.                               KITCHEN
000500       10  U-USAGE PIC 9(3) USAGE COMP.                           KITCHEN
000510* VALUE: both delimiters, doubled delimiters, a twenty-digit      KITCHEN
000520* number, signed decimals, figurative constants in either case.   KITCHEN
000530     05  VALUES-GROUP VALUE SPACES.                               KITCHEN
000540       10  V-APOS PIC X(3) VALUE 'ABC'.                           KITCHEN
000550       10  V-QUOT PIC X(3) VALUE "ABC".                           KITCHEN
000560       10  V-APOS-DOUBLED PIC X(4) VALUE 'IT''S'.                 KITCHEN
000570       10  V-QUOT-DOUBLED PIC X(6) VALUE "SAY ""HI""".            KITCHEN
000580       10  V-DIGITS PIC 9(18) VALUE 12345678901234567890.         KITCHEN
000590       10  V-PLUS PIC S9V9 VALUE +1.5.                            KITCHEN
000600       10  V-MINUS PIC S99V99 VALUE IS -12.50.                    KITCHEN
000610       10  V-ZERO PIC 9 VALUE ZERO.                               KITCHEN
000620       10  V-ZEROES PIC 9 VALUE zeroes.                           KITCHEN
000630       10  V-HIGH PIC X VALUE HIGH-VALUES.                        KITCHEN
000640       10  V-LOW PIC X VALUE low-value.                           KITCHEN
000650       10  V-QUOTE PIC X VALUE QUOTES.                            KITCHEN
000660       10  V-SPACE PIC X VALUES space.                            KITCHEN
000670* A literal continued across lines.                               KITCHEN
000680       10  V-CONTINUED PIC X(50) VALUE 'CONTINUED-AAAAAAAAAAAAAAAAKITCHEN
000690-    'BBBBBBBBBB'.                                                KITCHEN
000700* Condition-names: single, list, THRU and THROUGH, figurative.    KITCHEN
000710     05  STATUS-CODE PIC X.                                       KITCHEN
000720       88  STATUS-SINGLE VALUE 'A'.                               KITCHEN
000730       88  STATUS-LIST VALUES 'A' 'B' 'C'.                        KITCHEN
000740       88  STATUS-THRU VALUE 'D' THRU 'F' 'H' THROUGH 'K'.        KITCHEN
000750       88  STATUS-FIGURATIVE VALUE SPACE.                         KITCHEN
000760       88  STATUS-LOWER VALUE IS low-values 'Z'.                  KITCHEN
000770     05  STATUS-NUMBER PIC S99.                                   KITCHEN
000780       88  STATUS-NEGATIVE VALUE -99 THRU -1.                     KITCHEN
000790* OCCURS: fixed, a range, DEPENDING ON, keys and an index.        KITCHEN
000800     05  TABLE-COUNT PIC 9(2).                                    KITCHEN
000810     05  FIXED-TABLE OCCURS 3 TIMES.                              KITCHEN
000820       10  FIXED-CELL PIC X.                                      KITCHEN
000830     05  BARE-TABLE OCCURS 2.                                     KITCHEN
000840       10  BARE-CELL PIC X.                                       KITCHEN
000850     05  RANGE-TABLE OCCURS 1 TO 10 TIMES DEPENDING ON TABLE-COUNTKITCHEN
000860             ASCENDING KEY IS RANGE-KEY DESCENDING RANGE-ALT      KITCHEN
000870             ASCENDING KEY RANGE-OTHER INDEXED BY RANGE-IX.       KITCHEN
000880       10  RANGE-KEY PIC X.                                       KITCHEN
000890       10  RANGE-ALT PIC X.                                       KITCHEN
000900       10  RANGE-OTHER PIC X.                                     KITCHEN
000910     05  DEPENDS-TABLE OCCURS 0 TO 5 DEPENDING TABLE-COUNT        KITCHEN
000920             INDEXED DEPENDS-IX.                                  KITCHEN
000930       10  DEPENDS-CELL PIC X.                                    KITCHEN
000940* SIGN, SYNCHRONIZED, JUSTIFIED, BLANK WHEN ZERO.                 KITCHEN
000950     05  SIGNS.                                                   KITCHEN
000960       10  SIGN-LEADING PIC S9 SIGN LEADING.                      KITCHEN
000970       10  SIGN-TRAILING PIC S9 SIGN IS TRAILING.                 KITCHEN
000980       10  SIGN-LEADING-SEP PIC S9 SIGN LEADING SEPARATE.         KITCHEN
000990       10  SIGN-TRAILING-SEP PIC S9 SIGN TRAILING SEPARATE        KITCHEN
001000           CHARACTER.                                             KITCHEN
001010       10  SIGN-LOWER PIC S9 sign is leading separate character.  KITCHEN
001020     05  SYNCS.                                                   KITCHEN
001030       10  SYNC-BARE PIC S9(4) COMP SYNC.                         KITCHEN
001040       10  SYNC-LEFT PIC S9(4) COMP SYNCHRONIZED LEFT.            KITCHEN
001050       10  SYNC-RIGHT PIC S9(4) COMP SYNC RIGHT.                  KITCHEN
001060       10  SYNC-LOWER PIC S9(4) COMP sync left.                   KITCHEN
001070     05  JUSTS.                                                   KITCHEN
001080       10  JUST-BARE PIC X(4) JUST.                               KITCHEN
001090       10  JUST-RIGHT PIC X(4) JUSTIFIED RIGHT.                   KITCHEN
001100       10  BLANK-WHEN PIC 9(4) BLANK WHEN ZERO.                   KITCHEN
001110       10  BLANK-BARE PIC 9(4) BLANK ZERO.                        KITCHEN
001120* REDEFINES, a PICTURE spelled both ways, and clauses duplicated  KITCHEN
001130* and reordered.                                                  KITCHEN
001140     05  ORIGINAL PICTURE IS X(4).                                KITCHEN
001150     05  REDEFINER REDEFINES ORIGINAL PIC 9(4).                   KITCHEN
001160     05  DUPLICATED PIC X PIC X(2) USAGE DISPLAY DISPLAY.         KITCHEN
001170     05  REORDERED VALUE 'A' JUST PIC X GLOBAL.                   KITCHEN
001180     05  GROUP-REORDERED EXTERNAL REDEFINES ORIGINAL GLOBAL.      KITCHEN
001190       10  GROUP-CELL PIC X(4).                                   KITCHEN
001200* 66: a single name and a range, and one named FILLER.            KITCHEN
001210 66  SINK-ONE RENAMES ORIGINAL.                                   KITCHEN
001220 66  SINK-RANGE RENAMES LEVELS THRU STATUS-CODE.                  KITCHEN
001230 66  SINK-THROUGH RENAMES ORIGINAL THROUGH REDEFINER.             KITCHEN
001240 66  FILLER RENAMES ORIGINAL.                                     KITCHEN
001250* 77 with a condition-name.                                       KITCHEN
001260 77  STANDALONE PIC 9.                                            KITCHEN
001270     88  STANDALONE-ON VALUE 1.                                   KITCHEN
001280     88  STANDALONE-OFF VALUE 0.                                  KITCHEN
001290*
