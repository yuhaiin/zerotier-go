#ifndef ZTGO_H
#define ZTGO_H
#include <stdint.h>
#ifdef _WIN32
#define ZTGO_EXPORT __declspec(dllexport)
#else
#define ZTGO_EXPORT __attribute__((visibility("default")))
#endif
#ifdef __cplusplus
extern "C" {
#endif
/* Always fixed width: no C long, sockaddr_storage or C++ layout crosses FFI. */
typedef struct {uint8_t ip[16];uint32_t family;uint16_t port;uint16_t reserved;uint32_t scope;} ztgo_addr;
typedef struct {ztgo_addr target;ztgo_addr via;uint16_t flags;uint16_t metric;} ztgo_route;
typedef struct {uint64_t network_id;uint64_t mac;uint32_t operation;uint32_t status;uint32_t mtu;uint32_t addresses_count;uint32_t routes_count;char name[128];ztgo_addr addresses[32];ztgo_route routes[128];} ztgo_config;
typedef struct {uint64_t network_id;uint64_t source_mac;uint64_t dest_mac;uint32_t ether_type;uint32_t vlan;const void *data;uint32_t len;uint32_t reserved;} ztgo_frame;
typedef void (*ztgo_state_put)(uintptr_t,int32_t,uint64_t,uint64_t,const void*,int32_t);
typedef int32_t (*ztgo_state_get)(uintptr_t,int32_t,uint64_t,uint64_t,void*,uint32_t);
typedef int32_t (*ztgo_wire_send)(uintptr_t,const ztgo_addr*,const void*,uint32_t,uint32_t);
typedef void (*ztgo_frame_recv)(uintptr_t,const ztgo_frame*);
typedef int32_t (*ztgo_netconf_recv)(uintptr_t,const ztgo_config*);
typedef void (*ztgo_event_recv)(uintptr_t,int32_t);
typedef struct {ztgo_state_put put;ztgo_state_get get;ztgo_wire_send wire;ztgo_frame_recv frame;ztgo_netconf_recv config;ztgo_event_recv event;} ztgo_callbacks;
ZTGO_EXPORT uint32_t ztgo_abi_version(void);
ZTGO_EXPORT int32_t ztgo_new(void **out, uintptr_t user,const ztgo_callbacks *cbs,uint32_t flags,int64_t now);
ZTGO_EXPORT void ztgo_delete(void *h);
ZTGO_EXPORT uint64_t ztgo_address(void *h);
ZTGO_EXPORT int32_t ztgo_join(void *h,uint64_t network_id);
ZTGO_EXPORT int32_t ztgo_leave(void *h,uint64_t network_id);
ZTGO_EXPORT int32_t ztgo_orbit(void *h,uint64_t moon_id,uint64_t seed);
ZTGO_EXPORT int32_t ztgo_deorbit(void *h,uint64_t moon_id);
ZTGO_EXPORT int32_t ztgo_subscribe(void *h,uint64_t network_id,uint64_t mac,uint32_t adi);
ZTGO_EXPORT int32_t ztgo_unsubscribe(void *h,uint64_t network_id,uint64_t mac,uint32_t adi);
ZTGO_EXPORT int32_t ztgo_process_wire(void *h,int64_t now,const ztgo_addr *from,const void *data,uint32_t len,int64_t *deadline);
ZTGO_EXPORT int32_t ztgo_process_frame(void *h,int64_t now,const ztgo_frame *frame,int64_t *deadline);
ZTGO_EXPORT int32_t ztgo_tick(void *h,int64_t now,int64_t *deadline);
#ifdef __cplusplus
}
#endif
#endif