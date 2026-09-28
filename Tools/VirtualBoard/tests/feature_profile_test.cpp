#include "ProjectConfig.h"
#include "Project/FrontPanelModel.h"

static_assert(PCCONTROLLER_ENABLE_LOCAL_AUDIO_CUES == 1,
              "temporary recorder profile must preserve local audio");
static_assert(PCCONTROLLER_ENABLE_EEPROM_AUDIO_CUES == 1,
              "temporary recorder profile must preserve stored cues");
static_assert(menuPageNavigable(PAGE_RELAY) && menuPageNavigable(PAGE_KEYS) &&
                  menuPageNavigable(PAGE_ILLUMINATION) && menuPageNavigable(PAGE_SOUND),
              "required front-panel controls disappeared");

#if PCCONTROLLER_MACRO_STRIP_TEST
static_assert(!PCCONTROLLER_ENABLE_RF_LEARNING, "RF administration still enabled");
static_assert(!menuPageNavigable(PAGE_RF), "omitted RF editor remains navigable");
static_assert(canonicalMenuPage(PAGE_RF) == PAGE_DOOR,
              "a saved RF default page must fall back to an available page");
#else
static_assert(PCCONTROLLER_ENABLE_RF_LEARNING, "default profile lost RF administration");
static_assert(menuPageNavigable(PAGE_RF) && canonicalMenuPage(PAGE_RF) == PAGE_RF,
              "default profile lost the RF learning page");
#endif

int main() { return 0; }
