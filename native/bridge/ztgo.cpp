#include "ztgo.h"
#include "ZeroTierOne.h"
#include <cstring>
#include <new>
#ifdef _WIN32
#include <winsock2.h>
#include <ws2tcpip.h>
#else
#include <arpa/inet.h>
#endif

struct Bridge {
    ZT_Node *node;
    uintptr_t user;
    ztgo_callbacks callbacks;
};
static ztgo_addr unpack(const sockaddr_storage *s) {
    ztgo_addr a{};
    if(!s) return a;
    if(s->ss_family==AF_INET) {
        const sockaddr_in *x=reinterpret_cast<const sockaddr_in*>(s);
        a.family=4;a.port=ntohs(x->sin_port);std::memcpy(a.ip,&x->sin_addr,4);
    }else if(s->ss_family==AF_INET6){
        const sockaddr_in6 *x=reinterpret_cast<const sockaddr_in6*>(s);
        a.family=6;a.port=ntohs(x->sin6_port);a.scope=x->sin6_scope_id;
        std::memcpy(a.ip,&x->sin6_addr,16);
    }
    return a;
}
static bool pack(const ztgo_addr *a,sockaddr_storage *s) {
    std::memset(s,0,sizeof(*s));
    if(!a)return false;
    if(a->family==4) {
        auto *x=reinterpret_cast<sockaddr_in*>(s);
        x->sin_family=AF_INET;x->sin_port=htons(a->port);
        std::memcpy(&x->sin_addr,a->ip,4);return true;
    }
    if(a->family==6){
        auto *x=reinterpret_cast<sockaddr_in6*>(s);
        x->sin6_family=AF_INET6;x->sin6_port=htons(a->port);x->sin6_scope_id=a->scope;
        std::memcpy(&x->sin6_addr,a->ip,16);return true;
    }
    return false;
}
static void state_put(ZT_Node*,void *u,void*,ZT_StateObjectType typ,const uint64_t ids[2],const void*data,int len){
    auto *b=static_cast<Bridge*>(u);b->callbacks.put(b->user,int32_t(typ),ids[0],ids[1],data,len);
}
static int state_get(ZT_Node*,void *u,void*,ZT_StateObjectType typ,const uint64_t ids[2],void*data,unsigned int len){
    auto *b=static_cast<Bridge*>(u);return b->callbacks.get(b->user,int32_t(typ),ids[0],ids[1],data,len);
}
static int wire_send(ZT_Node*,void *u,void*,int64_t,const sockaddr_storage *remote,const void *pkt,unsigned int len,unsigned int ttl){
    auto *b=static_cast<Bridge*>(u);ztgo_addr a=unpack(remote);
    return b->callbacks.wire(b->user,&a,pkt,len,ttl);
}
static void frame_recv(ZT_Node*,void*u,void*,uint64_t nid,void**,uint64_t src,uint64_t dst,unsigned int eth,unsigned int vlan,const void*data,unsigned int len){
    auto *b=static_cast<Bridge*>(u);ztgo_frame f{nid,src,dst,eth,vlan,data,len,0};
    b->callbacks.frame(b->user,&f);
}
static int config_recv(ZT_Node*,void*u,void*,uint64_t nid,void**,ZT_VirtualNetworkConfigOperation op,const ZT_VirtualNetworkConfig *c){
    auto *b=static_cast<Bridge*>(u);ztgo_config v{};
    v.network_id=nid;v.operation=uint32_t(op);
    if(c){
        v.mac=c->mac;v.status=uint32_t(c->status);v.mtu=c->mtu;
        std::memcpy(v.name,c->name,sizeof(v.name)<sizeof(c->name)?sizeof(v.name):sizeof(c->name));
        v.addresses_count=c->assignedAddressCount < 32 ? c->assignedAddressCount : 32;
        for(uint32_t i=0;i<v.addresses_count;i++)v.addresses[i]=unpack(&c->assignedAddresses[i]);
        v.routes_count=c->routeCount < 128 ? c->routeCount : 128;
        for(uint32_t i=0;i<v.routes_count;i++){
            v.routes[i].target=unpack(&c->routes[i].target);
            v.routes[i].via=unpack(&c->routes[i].via);
            v.routes[i].flags=c->routes[i].flags;v.routes[i].metric=c->routes[i].metric;
        }
    }
    return b->callbacks.config(b->user,&v);
}
static void event_recv(ZT_Node*,void*u,void*,ZT_Event evt,const void*){
    auto *b=static_cast<Bridge*>(u);b->callbacks.event(b->user,int32_t(evt));
}
extern "C" {
ZTGO_EXPORT uint32_t ztgo_abi_version(void){return 1;}
ZTGO_EXPORT int32_t ztgo_new(void **out,uintptr_t user,const ztgo_callbacks *cbs,uint32_t flags,int64_t now){
    if(!out || !cbs || !cbs->put || !cbs->get || !cbs->wire || !cbs->frame || !cbs->config || !cbs->event) return -1;
    *out=nullptr;
    Bridge *b=new(std::nothrow) Bridge{};if(!b)return -1;
    b->user=user;b->callbacks=*cbs;
    ZT_Node_Callbacks callbacks{};
    callbacks.version=0;
    callbacks.statePutFunction=state_put;
    callbacks.stateGetFunction=state_get;
    callbacks.wirePacketSendFunction=wire_send;
    callbacks.virtualNetworkFrameFunction=frame_recv;
    callbacks.virtualNetworkConfigFunction=config_recv;
    callbacks.eventCallback=event_recv;
    ZT_Node_Config config{};
    config.lowBandwidthMode=(flags & 1)!=0;
    config.enableEncryptedHello=(flags & 2)!=0;
    const int32_t rc=ZT_Node_new(&b->node,&config,b,nullptr,&callbacks,now);
    if(rc!=ZT_RESULT_OK){delete b;return rc;}
    *out=b;return rc;
}
ZTGO_EXPORT void ztgo_delete(void*h){auto*b=static_cast<Bridge*>(h);if(b){ZT_Node_delete(b->node);delete b;}}
ZTGO_EXPORT uint64_t ztgo_address(void*h){return ZT_Node_address(static_cast<Bridge*>(h)->node);}
ZTGO_EXPORT int32_t ztgo_join(void*h,uint64_t nid){return ZT_Node_join(static_cast<Bridge*>(h)->node,nid,nullptr,nullptr);}
ZTGO_EXPORT int32_t ztgo_leave(void*h,uint64_t nid){return ZT_Node_leave(static_cast<Bridge*>(h)->node,nid,nullptr,nullptr);}
ZTGO_EXPORT int32_t ztgo_orbit(void*h,uint64_t moon,uint64_t seed){return ZT_Node_orbit(static_cast<Bridge*>(h)->node,nullptr,moon,seed);}
ZTGO_EXPORT int32_t ztgo_deorbit(void*h,uint64_t moon){return ZT_Node_deorbit(static_cast<Bridge*>(h)->node,nullptr,moon);}
ZTGO_EXPORT int32_t ztgo_subscribe(void*h,uint64_t network,uint64_t mac,uint32_t adi){return ZT_Node_multicastSubscribe(static_cast<Bridge*>(h)->node,nullptr,network,mac,adi);}
ZTGO_EXPORT int32_t ztgo_unsubscribe(void*h,uint64_t network,uint64_t mac,uint32_t adi){return ZT_Node_multicastUnsubscribe(static_cast<Bridge*>(h)->node,network,mac,adi);}
ZTGO_EXPORT int32_t ztgo_process_wire(void*h,int64_t now,const ztgo_addr*from,const void*data,uint32_t len,int64_t*deadline){
    sockaddr_storage s;if(!pack(from,&s))return -1;
    return ZT_Node_processWirePacket(static_cast<Bridge*>(h)->node,nullptr,now,0,&s,data,len,deadline);
}
ZTGO_EXPORT int32_t ztgo_process_frame(void*h,int64_t now,const ztgo_frame*frame,int64_t*deadline){
    if(!frame)return -1;
    return ZT_Node_processVirtualNetworkFrame(static_cast<Bridge*>(h)->node,nullptr,now,frame->network_id,frame->source_mac,frame->dest_mac,frame->ether_type,frame->vlan,frame->data,frame->len,deadline);
}
ZTGO_EXPORT int32_t ztgo_tick(void*h,int64_t now,int64_t*deadline){
    return ZT_Node_processBackgroundTasks(static_cast<Bridge*>(h)->node,nullptr,now,deadline);
}
}